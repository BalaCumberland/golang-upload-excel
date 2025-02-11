package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	_ "github.com/lib/pq"
	"github.com/xuri/excelize/v2"
)

// ✅ PostgreSQL Database Credentials
var (
	DBHost     = "mcq-db.clki64gmuinh.us-east-2.rds.amazonaws.com"
	DBPort     = "5432"
	DBUser     = "Kittu"
	DBPassword = "Kittussk99"
	DBName     = "mcqdb"
)

// ✅ Structs
type QuizData struct {
	QuizName  string     `json:"quizName"`
	Duration  int        `json:"duration"`
	Category  string     `json:"category"`
	Questions []Question `json:"questions"`
}

type Question struct {
	Hint             string `json:"hint"`
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
	// ✅ Start Transaction
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}

	// ✅ Prepare Dynamic Update Query
	query := "UPDATE students SET "
	params := []interface{}{student.Email} // Email is always first
	paramIndex := 2
	updateFields := []string{}

	if student.PhoneNumber != nil {
		updateFields = append(updateFields, fmt.Sprintf("phone_number = $%d", paramIndex))
		params = append(params, *student.PhoneNumber)
		paramIndex++
	}
	if student.Name != nil {
		updateFields = append(updateFields, fmt.Sprintf("name = $%d", paramIndex))
		params = append(params, *student.Name)
		paramIndex++
	}
	if student.StudentClass != nil {
		updateFields = append(updateFields, fmt.Sprintf("student_class = $%d", paramIndex))
		params = append(params, *student.StudentClass)
		paramIndex++
	}
	if student.PaymentStatus != nil {
		updateFields = append(updateFields, fmt.Sprintf("payment_status = $%d", paramIndex))
		params = append(params, *student.PaymentStatus)
		paramIndex++
	}

	// ✅ Always Update Timestamp
	updateFields = append(updateFields, "updated_time = NOW()")

	// ✅ If No Fields Provided, Return Error
	if len(updateFields) == 1 {
		tx.Rollback()
		return 0, fmt.Errorf("no valid fields to update")
	}

	// ✅ Construct Final Query
	query += fmt.Sprintf("%s WHERE email = $1", strings.Join(updateFields, ", "))
	result, err := tx.Exec(query, params...)
	if err != nil {
		tx.Rollback()
		return 0, err
	}

	// ✅ Commit Transaction
	err = tx.Commit()
	if err != nil {
		return 0, err
	}

	// ✅ Get Number of Updated Rows
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

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
			Hint:             row[0],
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
	lambda.Start(lambdaHandler)
}
