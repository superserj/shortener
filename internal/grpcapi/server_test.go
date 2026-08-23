package grpcapi

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/superserj/shortener/internal/auth"
	"github.com/superserj/shortener/internal/pb"
	"github.com/superserj/shortener/internal/service"
	"github.com/superserj/shortener/internal/storage"
)

const (
	testBaseURL = "http://localhost:8080"
	testSecret  = "test-secret"
	// bufSize — размер буфера соединения в тестах: сообщения сервиса короткие,
	// килобайта хватает с запасом
	bufSize = 1024 * 1024
)

// newTestClient поднимает службу на соединении в памяти и возвращает клиент к ней.
func newTestClient(t *testing.T, store storage.Repository) pb.ShortenerServiceClient {
	t.Helper()

	listener := bufconn.Listen(bufSize)
	log := zap.NewNop()
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(
		LoggingInterceptor(log),
		AuthInterceptor(auth.New(testSecret), log),
	))
	pb.RegisterShortenerServiceServer(srv, NewServer(service.New(store, testBaseURL, nil), log))

	go func() {
		_ = srv.Serve(listener)
	}()

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
		_ = listener.Close()
	})

	return pb.NewShortenerServiceClient(conn)
}

// shortenURL сокращает адрес и возвращает ссылку вместе с выданным токеном.
func shortenURL(ctx context.Context, t *testing.T, client pb.ShortenerServiceClient, url string) (string, string) {
	t.Helper()

	var header metadata.MD
	resp, err := client.ShortenURL(ctx,
		pb.URLShortenRequest_builder{Url: proto.String(url)}.Build(),
		grpc.Header(&header))
	require.NoError(t, err)

	var token string
	if values := header.Get(authMetadataKey); len(values) > 0 {
		token = values[0]
	}
	return resp.GetResult(), token
}

func TestShortenAndExpand(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t, storage.NewMemStorage())

	shortURL, token := shortenURL(ctx, t, client, "https://practicum.yandex.ru/")
	require.NotEmpty(t, token, "запросу без токена служба выдаёт новый")
	require.Contains(t, shortURL, testBaseURL+"/")

	id := shortURL[len(testBaseURL)+1:]
	resp, err := client.ExpandURL(ctx, pb.URLExpandRequest_builder{Id: proto.String(id)}.Build())
	require.NoError(t, err)
	assert.Equal(t, "https://practicum.yandex.ru/", resp.GetResult())
}

func TestShortenKnownURL(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t, storage.NewMemStorage())

	shortURL, _ := shortenURL(ctx, t, client, "https://practicum.yandex.ru/")

	_, err := client.ShortenURL(ctx,
		pb.URLShortenRequest_builder{Url: proto.String("https://practicum.yandex.ru/")}.Build())
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.AlreadyExists, st.Code())
	assert.Equal(t, shortURL, st.Message(), "в ошибке приходит выданная ранее ссылка")
}

func TestShortenEmptyURL(t *testing.T) {
	client := newTestClient(t, storage.NewMemStorage())

	_, err := client.ShortenURL(context.Background(),
		pb.URLShortenRequest_builder{Url: proto.String("   ")}.Build())
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestExpandMissingAndDeleted(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemStorage()
	require.NoError(t, store.Save(ctx, "deleted1", "https://example.com/gone", "user1"))
	require.NoError(t, store.MarkDeleted(ctx, "user1", []string{"deleted1"}))

	client := newTestClient(t, store)

	_, err := client.ExpandURL(ctx, pb.URLExpandRequest_builder{Id: proto.String("unknown1")}.Build())
	assert.Equal(t, codes.NotFound, status.Code(err))

	_, err = client.ExpandURL(ctx, pb.URLExpandRequest_builder{Id: proto.String("deleted1")}.Build())
	require.Equal(t, codes.NotFound, status.Code(err))
	assert.Equal(t, "url is deleted", status.Convert(err).Message())

	_, err = client.ExpandURL(ctx, pb.URLExpandRequest_builder{Id: proto.String("")}.Build())
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestListUserURLs(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t, storage.NewMemStorage())

	shortURL, token := shortenURL(ctx, t, client, "https://practicum.yandex.ru/")
	require.NotEmpty(t, token)

	authCtx := metadata.AppendToOutgoingContext(ctx, authMetadataKey, token)
	resp, err := client.ListUserURLs(authCtx, &emptypb.Empty{})
	require.NoError(t, err)

	require.Len(t, resp.GetUrl(), 1, "со своим токеном пользователь видит свою ссылку")
	assert.Equal(t, shortURL, resp.GetUrl()[0].GetShortUrl())
	assert.Equal(t, "https://practicum.yandex.ru/", resp.GetUrl()[0].GetOriginalUrl())

	// без токена запрос обслуживается как первый визит: ссылок у нового
	// пользователя нет
	resp, err = client.ListUserURLs(ctx, &emptypb.Empty{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetUrl())
}

func TestListUserURLsBrokenToken(t *testing.T) {
	ctx := metadata.AppendToOutgoingContext(context.Background(), authMetadataKey, "user1:deadbeef")
	client := newTestClient(t, storage.NewMemStorage())

	_, err := client.ListUserURLs(ctx, &emptypb.Empty{})
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "подделанный токен не пропускается")
}

func TestStorageFailure(t *testing.T) {
	ctx := context.Background()
	client := newTestClient(t, failingStore{})

	_, err := client.ShortenURL(ctx, pb.URLShortenRequest_builder{Url: proto.String("https://example.com/")}.Build())
	assert.Equal(t, codes.Internal, status.Code(err))

	_, err = client.ExpandURL(ctx, pb.URLExpandRequest_builder{Id: proto.String("id1")}.Build())
	assert.Equal(t, codes.Internal, status.Code(err))

	_, err = client.ListUserURLs(ctx, &emptypb.Empty{})
	assert.Equal(t, codes.Internal, status.Code(err))
}
