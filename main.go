package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"google.golang.org/api/option"

	firebase "firebase.google.com/go"
	"firebase.google.com/go/auth"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	_ "github.com/lib/pq"
)

var (
	firebaseAuth     *auth.Client
	db               *sql.DB
	userEmailContext string
)

func initFirebase() error {
	ctx := context.Background()
	credsJSON := os.Getenv("FIREBASE_SERVICE_ACCOUNT")
	if credsJSON == "" {
		return fmt.Errorf("FIREBASE_CREDENTIALS is not set")
	}
	var creds map[string]interface{}
	if err := json.Unmarshal([]byte(credsJSON), &creds); err != nil {
		return fmt.Errorf("failed to parse FIREBASE_CREDENTIALS: %v", err)
	}

	conf := &firebase.Config{}
	app, err := firebase.NewApp(ctx, conf, option.WithCredentialsJSON([]byte(credsJSON)))
	if err != nil {
		return fmt.Errorf("error initializing firebase app: %v", err)
	}
	firebaseAuth, err = app.Auth(ctx)
	if err != nil {
		return fmt.Errorf("error initializing firebase auth client: %v", err)
	}
	return nil
}

func verifyFirebaseToken(request events.LambdaFunctionURLRequest) (*auth.Token, error) {
	authHeader, ok := request.Headers["Authorization"]
	if !ok {
		authHeader, ok = request.Headers["authorization"]
	}
	if !ok || authHeader == "" {
		return nil, fmt.Errorf("missing Authorization header")
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		return nil, fmt.Errorf("invalid Authorization header format")
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")

	ctx := context.Background()
	token, err := firebaseAuth.VerifyIDToken(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify token: %v", err)
	}
	return token, nil
}

type DBCredentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	DBName   string `json:"dbname"`
}

func getDBCredentials() (*DBCredentials, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion("ap-south-1"))
	if err != nil {
		return nil, err
	}

	client := secretsmanager.NewFromConfig(cfg)
	
	result, err := client.GetSecretValue(context.TODO(), &secretsmanager.GetSecretValueInput{
		SecretId: aws.String("dbconfig"),
	})
	if err != nil {
		return nil, err
	}

	var creds DBCredentials
	err = json.Unmarshal([]byte(*result.SecretString), &creds)
	if err != nil {
		return nil, err
	}

	return &creds, nil
}

type QuizData struct {
	QuizName  string     `json:"quizName"`
	Duration  int        `json:"duration"`
	Category  string     `json:"category"`
	Questions []Question `json:"questions"`
}

type Question struct {
	Explanation      string `json:"explanation"`
	Question         string `json:"question"`
	CorrectAnswer    string `json:"correctAnswer"`
	IncorrectAnswers string `json:"incorrectAnswers"`
}

type StudentUpdateRequest struct {
	Email        string   `json:"email"`
	PhoneNumber  *string  `json:"phoneNumber,omitempty"`
	Name         *string  `json:"name,omitempty"`
	StudentClass *string  `json:"studentClass,omitempty"`
	Amount       *float64 `json:"amount,omitempty"`
	UpdatedBy    *string  `json:"updatedBy,omitempty"`
}

func initDB() error {
	creds, err := getDBCredentials()
	if err != nil {
		return fmt.Errorf("failed to get DB credentials: %v", err)
	}

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=require",
		creds.Host, creds.Port, creds.Username, creds.Password, creds.DBName)

	db, err = sql.Open("postgres", dsn)
	if err != nil {
		return err
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	return db.Ping()
}

func getDB() *sql.DB {
	return db
}

func getCORSHeaders() map[string]string {
	return map[string]string{
		"Access-Control-Allow-Origin":  "*",
		"Access-Control-Allow-Methods": "OPTIONS, POST, PUT",
		"Access-Control-Allow-Headers": "Content-Type, Authorization",
	}
}

func lambdaHandler(request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	log.Printf("Received request: Path = %s, Method = %s", request.RawPath, request.RequestContext.HTTP.Method)

	if request.RequestContext.HTTP.Method == "OPTIONS" {
		return events.LambdaFunctionURLResponse{
			StatusCode: 200,
			Headers:    getCORSHeaders(),
			Body:       `{"message":"CORS preflight response"}`,
		}, nil
	}

	publicPaths := map[string]bool{
		"/students/register": true,
	}
	if !publicPaths[request.RawPath] {
		token, err := verifyFirebaseToken(request)
		if err != nil {
			log.Printf("Authorization error: %v", err)
			return events.LambdaFunctionURLResponse{
				StatusCode: 401,
				Headers:    getCORSHeaders(),
				Body:       fmt.Sprintf(`{"error": "Unauthorized", "message": "%s"}`, err.Error()),
			}, nil
		}
		userEmailContext = token.Claims["email"].(string)
	}

	switch request.RawPath {
	case "/upload/questions":
		return handleQuizUpload(request)
	case "/students/update":
		return handleStudentUpdate(request)
	case "/students/get-by-email":
		return handleGetStudent(request)
	case "/students/register":
		return handleSaveStudent(request)
	case "/quiz/unattempted-quizzes":
		return handleGetUnattemptedQuizzes(request)
	case "/quiz/get-by-name":
		return handleGetQuizByName(request)
	default:
		log.Printf("Invalid API Path: %s", request.RawPath)
		return events.LambdaFunctionURLResponse{
			StatusCode: 404,
			Headers:    getCORSHeaders(),
			Body:       fmt.Sprintf(`{"error":"Invalid API endpoint", "receivedPath": "%s"}`, request.RawPath),
		}, nil
	}
}

func createErrorResponse(statusCode int, errorMessage string) events.LambdaFunctionURLResponse {
	return events.LambdaFunctionURLResponse{
		StatusCode: statusCode,
		Headers:    getCORSHeaders(),
		Body:       fmt.Sprintf(`{"error":"%s"}`, errorMessage),
	}
}

func main() {
	if err := initFirebase(); err != nil {
		log.Fatalf("Failed to initialize Firebase: %v", err)
	}
	if err := initDB(); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	lambda.Start(lambdaHandler)
}