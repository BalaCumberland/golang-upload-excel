package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
)

func handleGetQuizByName(request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	email := request.QueryStringParameters["email"]
	quizName := request.QueryStringParameters["quizName"]

	if email == "" || quizName == "" {
		return createErrorResponse(400, "Missing 'email' or 'quizName' parameter"), nil
	}

	userEmail := getUserEmail()
	if userEmail != "" && strings.ToLower(userEmail) != strings.ToLower(email) {
		return createErrorResponse(403, "Email in request body does not match authenticated user email"), nil
	}

	if !checkStudentPayment(strings.ToLower(email)) {
		return createErrorResponse(400, "Student not paid"), nil
	}

	var quiz Quiz
	var questionsJSON []byte
	query := `SELECT quiz_name, duration, category, questions FROM quiz_questions WHERE quiz_name = $1`
	
	err := getDB().QueryRow(query, quizName).Scan(&quiz.QuizName, &quiz.Duration, &quiz.Category, &questionsJSON)
	if err != nil {
		return createErrorResponse(404, fmt.Sprintf("Quiz not found: %s", quizName)), nil
	}

	if err := json.Unmarshal(questionsJSON, &quiz.Questions); err != nil {
		return createErrorResponse(500, "Internal Server Error"), err
	}

	tx, err := getDB().Begin()
	if err != nil {
		return createErrorResponse(500, "Internal Server Error"), err
	}
	defer tx.Rollback()

	updateQuery := `INSERT INTO student_quizzes (email, quiz_names) 
					VALUES ($1, to_jsonb(ARRAY[$2]::text[])) 
					ON CONFLICT (email) 
					DO UPDATE SET quiz_names = (
						SELECT jsonb_agg(DISTINCT q) 
						FROM jsonb_array_elements(
							COALESCE(student_quizzes.quiz_names, '[]'::jsonb) || to_jsonb(ARRAY[$2]::text[])
						) AS q
					)`
	
	_, err = tx.Exec(updateQuery, strings.ToLower(email), quizName)
	if err != nil {
		return createErrorResponse(500, "Internal Server Error"), err
	}

	if err := tx.Commit(); err != nil {
		return createErrorResponse(500, "Internal Server Error"), err
	}

	response := map[string]interface{}{
		"message": "Quiz fetched and updated successfully",
		"quiz":    quiz,
	}
	return createSuccessResponseData(response), nil
}

func handleGetUnattemptedQuizzes(request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	category := request.QueryStringParameters["category"]
	email := request.QueryStringParameters["email"]

	if category == "" || email == "" {
		return createErrorResponse(400, "Category and student email are required"), nil
	}

	userEmail := getUserEmail()
	if userEmail != "" && strings.ToLower(userEmail) != strings.ToLower(email) {
		return createErrorResponse(403, "Email in request body does not match authenticated user email"), nil
	}

	if !checkStudentPayment(strings.ToLower(email)) {
		return createErrorResponse(400, "Student not paid"), nil
	}

	query := `SELECT quiz_name FROM quiz_questions WHERE category = $1`
	args := []interface{}{category}

	if dateFilteredCategories[category] {
		now := time.Now()
		pattern := fmt.Sprintf("%s-%d-%d-%%", category, now.Month(), now.Day())
		query += ` AND quiz_name LIKE $2`
		args = append(args, pattern)
	}

	rows, err := getDB().Query(query, args...)
	if err != nil {
		return createErrorResponse(500, "Internal Server Error"), err
	}
	defer rows.Close()

	var allQuizzes []string
	for rows.Next() {
		var quizName string
		if err := rows.Scan(&quizName); err != nil {
			return createErrorResponse(500, "Internal Server Error"), err
		}
		allQuizzes = append(allQuizzes, quizName)
	}

	attemptedRows, err := getDB().Query(
		`SELECT jsonb_array_elements_text(quiz_names) AS quiz_name FROM student_quizzes WHERE LOWER(email) = $1`,
		strings.ToLower(email))
	if err != nil {
		return createErrorResponse(500, "Internal Server Error"), err
	}
	defer attemptedRows.Close()

	attemptedMap := make(map[string]bool)
	for attemptedRows.Next() {
		var quizName string
		if err := attemptedRows.Scan(&quizName); err != nil {
			return createErrorResponse(500, "Internal Server Error"), err
		}
		attemptedMap[quizName] = true
	}

	var unattempted []string
	for _, quiz := range allQuizzes {
		if !attemptedMap[quiz] {
			unattempted = append(unattempted, quiz)
		}
	}

	return createSuccessResponseData(map[string][]string{"unattempted_quizzes": unattempted}), nil
}