package grpcapi

import (
	"context"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/superserj/shortener/internal/auth"
)

// authMetadataKey — ключ метаданных с подписанным идентификатором пользователя,
// аналог куки в HTTP.
const authMetadataKey = "authorization"

// AuthInterceptor опознаёт пользователя по метаданным запроса. Запрос с
// испорченной подписью отклоняется, запросу без токена заводится новый
// пользователь, а сам токен уезжает клиенту в заголовках ответа — так же, как
// HTTP-мидлварь ставит куку.
func AuthInterceptor(a *auth.Authenticator, log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if values := md.Get(authMetadataKey); len(values) > 0 {
				userID, err := a.Verify(values[0])
				if err != nil {
					return nil, status.Error(codes.Unauthenticated, "invalid authorization token")
				}
				return handler(auth.WithUserID(ctx, userID), req)
			}
		}

		userID, err := auth.NewUserID()
		if err != nil {
			log.Error("issue user id", zap.Error(err))
			return nil, status.Error(codes.Internal, "failed to issue user id")
		}
		if err := grpc.SetHeader(ctx, metadata.Pairs(authMetadataKey, a.Sign(userID))); err != nil {
			log.Warn("send authorization metadata", zap.Error(err))
		}
		return handler(auth.WithUserID(ctx, userID), req)
	}
}

// LoggingInterceptor пишет в лог каждый обработанный вызов: метод, код ответа
// и время работы.
func LoggingInterceptor(log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		log.Info("grpc call",
			zap.String("method", info.FullMethod),
			zap.String("code", status.Code(err).String()),
			zap.Duration("duration", time.Since(start)),
		)
		return resp, err
	}
}
