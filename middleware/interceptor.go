package middleware

import (
	"context"
	"google.golang.org/grpc"
	"k8s.io/klog/v2"
)

// LogInterceptorMiddleware 记录日志
func LogInterceptorMiddleware() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			klog.Errorf("service %s called error:%v", info.FullMethod, err)
		} else {
			klog.Infof("service %s called", info.FullMethod)
		}

		return resp, err
	}
}

// RecoveryInterceptorMiddleware 防止panic导致整个服务崩溃
func RecoveryInterceptorMiddleware() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		defer func() {
			if err := recover(); err != nil {
				klog.Errorf("recovered from %v", err)
			}
		}()

		return handler(ctx, req)
	}
}
