package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	DB "microservices/auth-service/db"
	pb "microservices/auth-service/protobuf"
	"microservices/auth-service/util"
)

var (
	port = flag.Int("port", 3005, "The server port")
)

type authService struct {
	db *gorm.DB
	pb.UnimplementedAuthServiceServer
}

func (s *authService) RegisterUser(context context.Context, regForm *pb.UserRegistrationForm) (*pb.Token, error) {
	if len(regForm.Name) == 0 {
		return nil, status.Error(codes.InvalidArgument, "name can't be blank")
	}

	if len(regForm.Username) == 0 {
		return nil, status.Error(codes.InvalidArgument, "username can't be blank")
	}

	if len(regForm.Password) <= 5 {
		return nil, status.Error(codes.InvalidArgument, "password should have atleast 6 characters")
	}

	newToken := util.GenerateToken()
	result := s.db.Create(&DB.User{Name: regForm.Name, Password: regForm.Password, Username: regForm.Username, Token: newToken})
	if result.Error != nil {
		return nil, status.Error(codes.AlreadyExists, "username already exists")
	}
	return &pb.Token{Token: newToken}, nil
}

func (s *authService) LoginUser(context context.Context, creds *pb.UserCredentials) (*pb.Token, error) {
	var user DB.User
	result := s.db.First(&user, "username = ? AND password = ?", creds.Username, creds.Password)
	if result.Error != nil {
		return nil, status.Error(codes.Aborted, "invalid credentials")
	}
	return &pb.Token{Token: user.Token}, nil
}

func (s *authService) AuthenticateUser(context context.Context, token *pb.Token) (*pb.UserProfile, error) {
	var user DB.User
	result := s.db.First(&user, "token = ?", token.Token)
	if result.Error != nil {
		return nil, status.Error(codes.PermissionDenied, "invalid token")
	}
	return &pb.UserProfile{Name: user.Name, Username: user.Username}, nil
}

func main() {
	util.LoadEnv()
	db, err := DB.InitDb()
	if err != nil {
		log.Fatalf("error connecting to database: %v", err)
	}
	flag.Parse()

	lis, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	// OpenTelemetry setup
	res, _ := resource.New(context.Background(), resource.WithAttributes(semconv.ServiceName("auth-service")))
	exp, _ := otlptracegrpc.New(context.Background(), otlptracegrpc.WithEndpoint("otel-collector:4317"), otlptracegrpc.WithInsecure())
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exp), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	grpcServer := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler()))
	pb.RegisterAuthServiceServer(grpcServer, &authService{db: db})
	log.Printf("server listening at %v", lis.Addr())
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
