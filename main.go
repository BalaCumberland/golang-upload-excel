package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"google.golang.org/api/option"

	firebase "firebase.google.com/go"
	"firebase.google.com/go/auth"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	_ "github.com/lib/pq"
	"github.com/xuri/excelize/v2"
)

var firebaseAuth *auth.Client

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
	// Look for Authorization header (case-insensitive)
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

	// Verify the token using the Firebase Admin SDK
	ctx := context.Background()
	token, err := firebaseAuth.VerifyIDToken(ctx, idToken)
	if err != nil {
		return nil, fmt.Errorf("failed to verify token: %v", err)
	}
	return token, nil
}

// ✅ PostgreSQL Database Credentials
var (
	DBUser     = os.Getenv("POSTGRESQL_USER")
	DBHost     = os.Getenv("POSTGRESQL_HOST")
	DBName     = os.Getenv("POSTGRESQL_DB")
	DBPassword = os.Getenv("POSTGRESQL_PW")
	DBPort     = os.Getenv("POSTGRESQL_PORT")
)

// ✅ Structs
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
	Email         string  `json:"email"`
	PhoneNumber   *string `json:"phoneNumber,omitempty"`
	Name          *string `json:"name,omitempty"`
	StudentClass  *string `json:"studentClass,omitempty"`
	PaymentStatus *string `json:"paymentStatus,omitempty"`
	UpdatedBy     *string `json:"updatedBy,omitempty"`
}

// ✅ Connect to PostgreSQL
func connectDB() (*sql.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=require",
		DBHost, DBPort, DBUser, DBPassword, DBName)
	return sql.Open("postgres", dsn)
}

// ✅ CORS Headers Helper Function
func getCORSHeaders() map[string]string {
	return map[string]string{
		"Access-Control-Allow-Origin":  "*",
		"Access-Control-Allow-Methods": "OPTIONS, POST, PUT",
		"Access-Control-Allow-Headers": "Content-Type, Authorization",
	}
}

// ✅ AWS Lambda Handler for Function URLs
func lambdaHandler(request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	log.Printf("📌 Received request: Path = %s, Method = %s", request.RawPath, request.RequestContext.HTTP.Method)

	// ✅ Handle CORS Preflight
	if request.RequestContext.HTTP.Method == "OPTIONS" {
		return events.LambdaFunctionURLResponse{
			StatusCode: 200,
			Headers:    getCORSHeaders(),
			Body:       `{"message":"CORS preflight response"}`,
		}, nil
	}

	// ✅ Verify Firebase ID Token
	_, err := verifyFirebaseToken(request)
	if err != nil {
		log.Printf("❌ Authorization error: %v", err)
		return events.LambdaFunctionURLResponse{
			StatusCode: 401,
			Headers:    getCORSHeaders(),
			Body:       fmt.Sprintf(`{"error": "Unauthorized", "message": "%s"}`, err.Error()),
		}, nil
	}

	// ✅ Route API Requests
	switch request.RawPath {
	case "/upload/questions":
		return handleQuizUpload(request)
	case "/students/update":
		return handleStudentUpdate(request)
	default:
		log.Printf("❌ Invalid API Path: %s", request.RawPath)
		return events.LambdaFunctionURLResponse{
			StatusCode: 404,
			Headers:    getCORSHeaders(),
			Body:       fmt.Sprintf(`{"error":"Invalid API endpoint", "receivedPath": "%s"}`, request.RawPath),
		}, nil
	}
}

// ✅ Handle Student Update
func handleStudentUpdate(request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	var studentUpdate StudentUpdateRequest

	err := json.Unmarshal([]byte(request.Body), &studentUpdate)
	if err != nil {
		log.Println("❌ Error parsing JSON:", err)
		return createErrorResponse(400, "Invalid JSON format"), nil
	}

	// ✅ Validate Required Fields
	if studentUpdate.Email == "" {
		return createErrorResponse(400, "Missing 'email' parameter"), nil
	}

	// ✅ Connect to Database
	db, err := connectDB()
	if err != nil {
		log.Println("❌ Database connection error:", err)
		return createErrorResponse(500, "Database connection failed"), nil
	}
	defer db.Close()

	// ✅ Perform Partial Update
	rowsAffected, err := updateStudent(db, studentUpdate)
	if err != nil {
		log.Println("❌ Error updating student:", err)
		return createErrorResponse(500, "Internal server error"), nil
	}

	// ✅ Handle No Matching Record
	if rowsAffected == 0 {
		return createErrorResponse(404, "No student found with the provided email"), nil
	}

	// ✅ Success Response
	return createSuccessResponse("Student updated successfully"), nil
}

func updateStudent(db *sql.DB, student StudentUpdateRequest) (int64, error) {
	// ✅ Convert email to lowercase for case-insensitive comparison
	normalizedEmail := strings.ToLower(student.Email)

	log.Printf("🔍 Updating student: Email = %s", normalizedEmail)

	// ✅ Check existing payment status before updating
	var existingPaymentStatus string
	err := db.QueryRow("SELECT payment_status FROM students WHERE LOWER(email) = $1", normalizedEmail).Scan(&existingPaymentStatus)
	if err != nil {
		log.Printf("❌ Failed to fetch existing payment status for email %s: %v", normalizedEmail, err)
		return 0, fmt.Errorf("failed to fetch existing payment status: %w", err)
	}

	log.Printf("✅ Existing payment status: %s", existingPaymentStatus)

	// ✅ Start Transaction
	tx, err := db.Begin()
	if err != nil {
		log.Printf("❌ Failed to begin transaction for email %s: %v", normalizedEmail, err)
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Rollback if an error occurs

	// ✅ Prepare Dynamic Update Query
	query := "UPDATE students SET "
	params := []interface{}{normalizedEmail} // Email is always first
	paramIndex := 2
	updateFields := []string{}
	updatePaymentTime := false // ✅ Track if `payment_time` should be updated

	if student.PhoneNumber != nil && *student.PhoneNumber != "" {
		log.Printf("📞 Updating phone number: %s", *student.PhoneNumber)
		updateFields = append(updateFields, fmt.Sprintf("phone_number = $%d", paramIndex))
		params = append(params, *student.PhoneNumber)
		paramIndex++
	}
	if student.Name != nil && *student.Name != "" {
		log.Printf("📝 Updating name: %s", *student.Name)
		updateFields = append(updateFields, fmt.Sprintf("name = $%d", paramIndex))
		params = append(params, *student.Name)
		paramIndex++
	}
	if student.StudentClass != nil && *student.StudentClass != "" {
		log.Printf("🏫 Updating student class: %s", *student.StudentClass)
		updateFields = append(updateFields, fmt.Sprintf("student_class = $%d", paramIndex))
		params = append(params, *student.StudentClass)
		paramIndex++
	}
	if student.PaymentStatus != nil && *student.PaymentStatus != "" {
		log.Printf("💳 Updating payment status: %s", *student.PaymentStatus)
		updateFields = append(updateFields, fmt.Sprintf("payment_status = $%d", paramIndex))
		params = append(params, *student.PaymentStatus)
		paramIndex++

		// ✅ Update `payment_time` if `payment_status` is changing to "PAID" and wasn't "PAID" before
		if existingPaymentStatus != "PAID" && *student.PaymentStatus == "PAID" {
			log.Printf("⏳ Payment status changed to PAID, updating payment_time")
			updatePaymentTime = true
			updateFields = append(updateFields, fmt.Sprintf("updated_by = $%d", paramIndex))
			params = append(params, *student.UpdatedBy)
			paramIndex++
		}
	}

	// ✅ Update `payment_time` only when payment_status is set to "PAID"
	if updatePaymentTime {
		updateFields = append(updateFields, "payment_time = NOW()")
	}

	// ✅ If No Fields Provided, Return Error
	if len(updateFields) == 0 {
		log.Printf("⚠️ No valid fields to update for email: %s", normalizedEmail)
		return 0, fmt.Errorf("no valid fields to update")
	}

	// ✅ Construct Final Query
	query += fmt.Sprintf("%s WHERE LOWER(email) = $1", strings.Join(updateFields, ", "))

	log.Printf("📡 Executing query: %s", query)

	// ✅ Execute Query
	result, err := tx.Exec(query, params...)
	if err != nil {
		log.Printf("❌ Failed to execute update for email %s: %v", normalizedEmail, err)
		return 0, fmt.Errorf("failed to execute update: %w", err)
	}

	// ✅ Commit Transaction
	err = tx.Commit()
	if err != nil {
		log.Printf("❌ Failed to commit transaction for email %s: %v", normalizedEmail, err)
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// ✅ Get Number of Updated Rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		log.Printf("❌ Failed to fetch affected rows for email %s: %v", normalizedEmail, err)
		return 0, fmt.Errorf("failed to fetch affected rows: %w", err)
	}

	log.Printf("✅ Successfully updated %d row(s) for email %s", rowsAffected, normalizedEmail)
	return rowsAffected, nil
}

// ✅ Handle Quiz Upload
func handleQuizUpload(request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	queryParams := request.QueryStringParameters
	category := queryParams["category"]
	durationStr := queryParams["duration"]
	quizName := queryParams["quizName"]

	if category == "" || durationStr == "" || quizName == "" {
		return createErrorResponse(400, "Missing required query parameters"), nil
	}

	duration, err := strconv.Atoi(durationStr)
	if err != nil {
		return createErrorResponse(400, "Invalid duration format"), nil
	}

	fileContent, err := base64.StdEncoding.DecodeString(request.Body)
	if err != nil {
		return createErrorResponse(400, "Invalid file encoding"), nil
	}

	quizData, err := processExcel(fileContent, category, duration, quizName)
	if err != nil {
		return createErrorResponse(500, "Failed to process Excel file"), nil
	}

	err = saveToPostgres(quizData)
	if err != nil {
		return createErrorResponse(500, "Failed to save to database"), nil
	}

	return createSuccessResponse("Quiz uploaded successfully"), nil
}

// ✅ Process Excel File
func processExcel(fileBytes []byte, category string, duration int, quizName string) (QuizData, error) {
	f, err := excelize.OpenReader(bytes.NewReader(fileBytes))
	if err != nil {
		return QuizData{}, err
	}

	sheetName := f.GetSheetName(0)
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return QuizData{}, err
	}

	var questions []Question
	for _, row := range rows[1:] {
		if len(row) < 4 {
			continue
		}
		questions = append(questions, Question{
			Explanation:      row[0],
			Question:         row[1],
			CorrectAnswer:    row[2],
			IncorrectAnswers: row[3],
		})
	}

	return QuizData{QuizName: quizName, Duration: duration, Category: category, Questions: questions}, nil
}

// ✅ Utility: Create Success Response
func createSuccessResponse(message string) events.LambdaFunctionURLResponse {
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers:    getCORSHeaders(),
		Body:       fmt.Sprintf(`{"message":"%s"}`, message),
	}
}

// ✅ Utility: Create Error Response
func createErrorResponse(statusCode int, errorMessage string) events.LambdaFunctionURLResponse {
	return events.LambdaFunctionURLResponse{
		StatusCode: statusCode,
		Headers:    getCORSHeaders(),
		Body:       fmt.Sprintf(`{"error":"%s"}`, errorMessage),
	}
}

// ✅ Save Data to PostgreSQL
func saveToPostgres(quiz QuizData) error {
	db, err := connectDB()
	if err != nil {
		return err
	}
	defer db.Close()

	questionsJSON, err := json.Marshal(quiz.Questions)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO quiz_questions (quiz_name, duration, category, questions)
		VALUES ($1, $2, $3, $4::jsonb)
		ON CONFLICT (quiz_name)
		DO UPDATE SET duration = EXCLUDED.duration, category = EXCLUDED.category, questions = EXCLUDED.questions;
	`

	_, err = db.Exec(query, quiz.QuizName, quiz.Duration, quiz.Category, questionsJSON)
	return err
}

// ✅ Main Function
func main() {
	if err := initFirebase(); err != nil {
		log.Fatalf("Failed to initialize Firebase: %v", err)
	}
	lambda.Start(lambdaHandler)
}
