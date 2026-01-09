package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"time"

	availabilityv1 "bronivik/internal/api/gen/availability/v1"
	"bronivik/internal/config"
	"bronivik/internal/database"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

type GRPCServer struct {
	cfg      *config.APIConfig
	db       *database.DB
	server   *grpc.Server
	listener net.Listener
	log      zerolog.Logger
}

func NewGRPCServer(cfg *config.APIConfig, db *database.DB, logger *zerolog.Logger) (*GRPCServer, error) {
	addr := fmt.Sprintf(":%d", cfg.GRPC.Port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("grpc listen %s: %w", addr, err)
	}

	auth := NewAuthInterceptor(cfg)
	unary := ChainUnaryInterceptors(
		LoggingUnaryInterceptor(logger),
		auth.Unary(),
	)

	serverOpts := []grpc.ServerOption{grpc.UnaryInterceptor(unary)}
	if cfg.GRPC.TLS.Enabled {
		tlsCfg, err := buildTLSConfig(cfg.GRPC.TLS)
		if err != nil {
			return nil, err
		}
		serverOpts = append(serverOpts, grpc.Creds(credentials.NewTLS(tlsCfg)))
	}

	grpcServer := grpc.NewServer(serverOpts...)

	svc := NewAvailabilityService(db)
	availabilityv1.RegisterAvailabilityServiceServer(grpcServer, svc)

	if cfg.GRPC.Reflection {
		reflection.Register(grpcServer)
	}

	var serverLogger zerolog.Logger
	if logger != nil {
		serverLogger = logger.With().Str("component", "grpc").Logger()
	}

	return &GRPCServer{
		cfg:      cfg,
		db:       db,
		server:   grpcServer,
		listener: lis,
		log:      serverLogger,
	}, nil
}

func buildTLSConfig(cfg config.APITLSConfig) (*tls.Config, error) {
	if cfg.CertFile == "" || cfg.KeyFile == "" {
		return nil, fmt.Errorf("grpc tls enabled but cert_file/key_file not set")
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load grpc tls keypair: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	if cfg.RequireClientCert {
		if cfg.ClientCAFile == "" {
			return nil, fmt.Errorf("grpc tls require_client_cert=true but client_ca_file not set")
		}
		caPEM, err := os.ReadFile(cfg.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read client_ca_file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("failed to parse client_ca_file PEM")
		}
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		tlsCfg.ClientCAs = pool
	}

	return tlsCfg, nil
}

func (s *GRPCServer) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

func (s *GRPCServer) Serve() error {
	s.log.Info().Str("addr", s.Addr()).Msg("gRPC API listening")
	return s.server.Serve(s.listener)
}

func (s *GRPCServer) Shutdown(ctx context.Context) {
	if s.server == nil {
		return
	}

	done := make(chan struct{})
	go func() {
		s.server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		return
	case <-ctx.Done():
		s.log.Warn().Msg("gRPC graceful shutdown timed out; forcing stop")
		s.server.Stop()
		return
	case <-time.After(10 * time.Second):
		s.log.Warn().Msg("gRPC graceful shutdown timed out; forcing stop")
		s.server.Stop()
		return
	}
}
