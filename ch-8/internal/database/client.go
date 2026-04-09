package database

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Ensure grpcRepository implements Repository
var _ Repository = (*grpcRepository)(nil)

type grpcRepository struct {
	client RepositoryClient
	conn   *grpc.ClientConn
}

// NewGRPCClient creates a Repository that connects to the Database service via gRPC
func NewGRPCClient(addr string) (Repository, error) {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("creating database service client: %w", err)
	}

	return &grpcRepository{
		client: NewRepositoryClient(conn),
		conn:   conn,
	}, nil
}

func (g *grpcRepository) GetStats(ctx context.Context) (*Stats, error) {
	resp, err := g.client.GetStats(ctx, &GetStatsRequest{})
	if err != nil {
		return nil, fmt.Errorf("getting stats via RPC: %w", err)
	}

	return &Stats{
		Messages: int(resp.Messages),
		Users:    int(resp.Users),
		Bots:     int(resp.Bots),
		Servers:  int(resp.Servers),
	}, nil
}

func (g *grpcRepository) ValidateLogin(ctx context.Context, username string, password string) error {
	_, err := g.client.ValidateLogin(ctx, &ValidateLoginRequest{
		Username: username,
		Password: password,
	})
	if err != nil {
		return fmt.Errorf("validating login via RPC: %w", err)
	}
	return nil
}

func (g *grpcRepository) UpdateDatabase(ctx context.Context, s ...StatsUpdate) (int, error) {
	updates := make([]*StatsUpdateMsg, len(s))
	for i, record := range s {
		updates[i] = &StatsUpdateMsg{
			Id:     record.Id,
			User:   record.User,
			Server: record.Server,
			IsBot:  record.IsBot,
		}
	}
	resp, err := g.client.UpdateDatabase(ctx, &UpdateDatabaseRequest{
		StatsUpdate: updates,
	})
	if err != nil {
		return 0, fmt.Errorf("updating database via RPC: %w", err)
	}
	return int(resp.GetCount()), nil
}

// Do not expose to gRPC clients
func (g *grpcRepository) MigrateDatabase(ctx context.Context) error {
	return nil
}

// Do not expose to gRPC clients
func (g *grpcRepository) AddUser(ctx context.Context, username string, password string) error {
	return nil
}

// Close connection, server continues to run
func (g *grpcRepository) Close() error {
	return g.conn.Close()
}
