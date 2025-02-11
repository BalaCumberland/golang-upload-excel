package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"regexp"
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
		"Access-Control-Allow-Methods": "OPTIONS, POST, GET",
		"Access-Control-Allow-Headers": "Content-Type, Authorization",
	}
}

// ✅ Validate File Name Format
func isValidFilename(filename string) (bool, string) {
	nameWithoutExt := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	regexPattern := `^((DEMO|CLS\d{2}-[A-Z]+-[A-Z]+)-\d{2}-\d{2}-.+)$`
	matched, _ := regexp.MatchString(regexPattern, nameWithoutExt)
	if matched {
		return true, nameWithoutExt
	}
	return false, ""
}

// ✅ AWS Lambda Handler
func lambdaHandler(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// ✅ Handle CORS Preflight Request
	if request.HTTPMethod == "OPTIONS" {
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
			Headers:    getCORSHeaders(),
			Body:       `{"message":"CORS preflight response"}`,
		}, nil
	}

	// ✅ Extract Query Parameters
	category, ok := request.QueryStringParameters["category"]
	if !ok {
		log.Println("❌ Missing category parameter")
		return events.APIGatewayProxyResponse{StatusCode: 400, Headers: getCORSHeaders(), Body: `{"error":"Missing category parameter"}`}, nil
	}

	durationStr, ok := request.QueryStringParameters["duration"]
	if !ok {
		log.Println("❌ Missing duration parameter")
		return events.APIGatewayProxyResponse{StatusCode: 400, Headers: getCORSHeaders(), Body: `{"error":"Missing duration parameter"}`}, nil
	}

	quizName, hasQuizName := request.QueryStringParameters["quizName"]
	if !hasQuizName {
		log.Println("❌ Missing quizName parameter")
		return events.APIGatewayProxyResponse{StatusCode: 400, Headers: getCORSHeaders(), Body: `{"error":"Missing quizName parameter"}`}, nil
	}

	duration, err := strconv.Atoi(durationStr)
	if err != nil {
		log.Println("❌ Error parsing duration:", err)
		return events.APIGatewayProxyResponse{StatusCode: 400, Headers: getCORSHeaders(), Body: `{"error":"Invalid duration format"}`}, nil
	}

	// ✅ Extract File Content from Base64
	if request.Body == "" {
		log.Println("❌ No file provided")
		return events.APIGatewayProxyResponse{StatusCode: 400, Headers: getCORSHeaders(), Body: `{"error":"No file uploaded"}`}, nil
	}

	fileContent, err := base64.StdEncoding.DecodeString(request.Body)
	if err != nil {
		log.Println("❌ Error decoding file:", err)
		return events.APIGatewayProxyResponse{StatusCode: 400, Headers: getCORSHeaders(), Body: `{"error":"Invalid file encoding"}`}, nil
	}

	// ✅ Process Excel File
	quizData, err := processExcel(fileContent, category, duration, quizName)
	if err != nil {
		log.Println("❌ Error processing Excel file:", err)
		return events.APIGatewayProxyResponse{StatusCode: 500, Headers: getCORSHeaders(), Body: `{"error":"Failed to process Excel file"}`}, nil
	}

	// ✅ Save to PostgreSQL
	err = saveToPostgres(quizData)
	if err != nil {
		log.Println("❌ Error saving to database:", err)
		return events.APIGatewayProxyResponse{StatusCode: 500, Headers: getCORSHeaders(), Body: `{"error":"Failed to save to database"}`}, nil
	}

	return events.APIGatewayProxyResponse{StatusCode: 200, Headers: getCORSHeaders(), Body: `{"message":"Quiz uploaded successfully"}`}, nil
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
	for _, row := range rows[1:] { // Skipping header row
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

	return QuizData{
		QuizName:  quizName,
		Duration:  duration,
		Category:  category,
		Questions: questions,
	}, nil
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
