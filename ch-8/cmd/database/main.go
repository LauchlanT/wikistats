package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"wikistats/internal/config"
	"wikistats/internal/database"

	"google.golang.org/grpc"
)

// Implement interface for responding to gRPC calls
type repositoryServer struct {
	database.UnimplementedRepositoryServer
	db database.Repository
}

func (s *repositoryServer) UpdateDatabase(ctx context.Context, req *database.UpdateDatabaseRequest) (*database.UpdateDatabaseResponse, error) {
	err := s.db.UpdateDatabase(ctx, database.StatsUpdate{
		Id:     req.StatsUpdate.Id,
		User:   req.StatsUpdate.User,
		Server: req.StatsUpdate.Server,
		IsBot:  req.StatsUpdate.IsBot,
	})
	if err != nil {
		return nil, err
	}
	return &database.UpdateDatabaseResponse{Success: true}, nil
}

func (s *repositoryServer) GetStats(ctx context.Context, req *database.GetStatsRequest) (*database.GetStatsResponse, error) {
	stats, err := s.db.GetStats(ctx)
	if err != nil {
		return nil, err
	}
	return &database.GetStatsResponse{
		Messages: int64(stats.Messages),
		Users:    int64(stats.Users),
		Bots:     int64(stats.Bots),
		Servers:  int64(stats.Servers),
	}, nil
}

func (s *repositoryServer) ValidateLogin(ctx context.Context, req *database.ValidateLoginRequest) (*database.ValidateLoginResponse, error) {
	err := s.db.ValidateLogin(ctx, req.Username, req.Password)
	if err != nil {
		return &database.ValidateLoginResponse{Valid: false}, err
	}
	return &database.ValidateLoginResponse{Valid: true}, nil
}

func main() {
	if err := run(); err != nil {
		log.Printf("Database error: %v", err)
		os.Exit(1)
	}
}

func run() error {
	envFile := flag.String("env", ".env", "override path to environment variables file")
	flag.Parse()
	if *envFile != "" {
		if err := config.LoadEnv(*envFile); err != nil {
			log.Printf("Could not load env file: %v", err)
		}
	}
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := database.New(cfg.Database)
	if err != nil {
		return fmt.Errorf("initializing database: %w", err)
	}
	defer func(db database.Repository) {
		err = errors.Join(err, db.Close())
	}(db)
	if err := db.MigrateDatabase(ctx); err != nil {
		return fmt.Errorf("migrating database: %w", err)
	}
	if err := db.AddUser(ctx, cfg.API.Username, cfg.API.Password); err != nil {
		return fmt.Errorf("creating user account: %w", err)
	}

	grpcServer := grpc.NewServer()
	database.RegisterRepositoryServer(grpcServer, &repositoryServer{db: db})
	listener, err := net.Listen("tcp", ":"+cfg.Database.Port)
	if err != nil {
		return fmt.Errorf("connecting to port: %w", err)
	}
	serverErr := make(chan error, 1)
	go func() {
		log.Println("gRPC server starting")
		serverErr <- grpcServer.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		log.Println("Shutdown signal received, stopping services...")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.Main.ShutdownTimeout)
		defer shutdownCancel()

		stopped := make(chan struct{})
		go func() {
			grpcServer.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
			log.Println("gRPC server stopped gracefully")
		case <-shutdownCtx.Done():
			log.Println("Forced shutdown of gRPC server")
			grpcServer.Stop()
		}
		if serveErr := <-serverErr; serveErr != nil {
			return fmt.Errorf("server error: %w", serveErr)
		}
		log.Println("Database terminated")
		return nil
	case err := <-serverErr:
		if err != nil {
			return fmt.Errorf("gRPC server error: %w", err)
		}
		return nil
	}

}
