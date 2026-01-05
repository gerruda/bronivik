package api

import (
	"context"

	"google.golang.org/grpc"
)

func ChainUnaryInterceptors(interceptors ...grpc.UnaryServerInterceptor) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		chained := handler
		for i := len(interceptors) - 1; i >= 0; i-- {
			current := interceptors[i]
			next := chained
			chained = func(currentCtx context.Context, currentReq any) (any, error) {
				return current(currentCtx, currentReq, info, next)
			}
		}
		return chained(ctx, req)
	}
}
