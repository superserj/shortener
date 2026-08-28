// Пакет grpcapi отдаёт по gRPC те же операции над ссылками, что сервис отдаёт
// по HTTP. Вся логика остаётся в пакете service, здесь только преобразование
// запросов и ответов.
package grpcapi

import (
	"context"
	"errors"
	"strings"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/pb"
	"github.com/superserj/shortener/internal/service"
	"github.com/superserj/shortener/internal/storage"
)

// Server реализует службу ShortenerService.
type Server struct {
	pb.UnimplementedShortenerServiceServer

	svc *service.Service
	log *zap.Logger
}

// NewServer создаёт реализацию службы поверх сервиса ссылок.
func NewServer(svc *service.Service, log *zap.Logger) *Server {
	return &Server{svc: svc, log: log}
}

// ShortenURL сокращает адрес. Для адреса, который уже сокращали, возвращает
// codes.AlreadyExists с выданной ранее ссылкой в тексте ошибки: в gRPC это
// единственный способ отдать ссылку так же, как HTTP отдаёт её с кодом 409.
func (s *Server) ShortenURL(ctx context.Context, in *pb.URLShortenRequest) (*pb.URLShortenResponse, error) {
	originalURL := strings.TrimSpace(in.GetUrl())
	if originalURL == "" {
		return nil, status.Error(codes.InvalidArgument, "url is empty")
	}

	userID, _ := auth.UserIDFromContext(ctx)
	shortURL, conflict, err := s.svc.Shorten(ctx, originalURL, userID)
	if err != nil {
		s.log.Warn("save failed", zap.Error(err))
		return nil, status.Error(codes.Internal, "save failed")
	}
	if conflict {
		return nil, status.Error(codes.AlreadyExists, shortURL)
	}

	return pb.URLShortenResponse_builder{Result: proto.String(shortURL)}.Build(), nil
}

// ExpandURL возвращает оригинальный адрес по короткой ссылке. Отдельного кода
// для удалённой ссылки в gRPC нет, поэтому она, как и неизвестная, отвечает
// codes.NotFound — различает их текст ошибки.
func (s *Server) ExpandURL(ctx context.Context, in *pb.URLExpandRequest) (*pb.URLExpandResponse, error) {
	id := strings.TrimSpace(in.GetId())
	if id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is empty")
	}

	userID, _ := auth.UserIDFromContext(ctx)
	originalURL, err := s.svc.Expand(ctx, id, userID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		return nil, status.Error(codes.NotFound, "url is not found")
	case errors.Is(err, storage.ErrDeleted):
		return nil, status.Error(codes.NotFound, "url is deleted")
	case err != nil:
		s.log.Warn("get failed", zap.Error(err))
		return nil, status.Error(codes.Internal, "get failed")
	}

	return pb.URLExpandResponse_builder{Result: proto.String(originalURL)}.Build(), nil
}

// ListUserURLs возвращает ссылки текущего пользователя. Пользователя опознаёт
// перехватчик, поэтому здесь остаётся только проверить, что он известен.
func (s *Server) ListUserURLs(ctx context.Context, _ *pb.ListUserURLsRequest) (*pb.UserURLsResponse, error) {
	userID, ok := auth.UserIDFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "user is unknown")
	}

	urls, err := s.svc.ListUserURLs(ctx, userID)
	if err != nil {
		s.log.Warn("list by user failed", zap.Error(err))
		return nil, status.Error(codes.Internal, "list failed")
	}

	data := make([]*pb.URLData, 0, len(urls))
	for _, u := range urls {
		data = append(data, pb.URLData_builder{
			ShortUrl:    proto.String(u.ShortURL),
			OriginalUrl: proto.String(u.OriginalURL),
		}.Build())
	}
	return pb.UserURLsResponse_builder{Url: data}.Build(), nil
}
