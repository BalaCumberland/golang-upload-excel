package main

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
)

func handleGetStudent(request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	email := request.QueryStringParameters["email"]
	if email == "" {
		return createErrorResponse(400, "Missing 'email' query parameter"), nil
	}

	userEmail := getUserEmail()
	if userEmail != "" && !contains(allowedEmails, strings.ToLower(userEmail)) {
		return createErrorResponse(403, "Email in request body is not authorized"), nil
	}

	var student Student
	query := `SELECT id, email, name, student_class, phone_number, sub_exp_date, updated_by, amount, payment_time, role 
			  FROM students WHERE LOWER(email) = LOWER($1)`
	
	row := getDB().QueryRow(query, strings.ToLower(email))
	err := row.Scan(&student.ID, &student.Email, &student.Name, &student.StudentClass, 
		&student.PhoneNumber, &student.SubExpDate, &student.UpdatedBy, &student.Amount, 
		&student.PaymentTime, &student.Role)
	
	if err == sql.ErrNoRows {
		return createErrorResponse(404, "Student not found"), nil
	}
	if err != nil {
		return createErrorResponse(500, "Internal Server Error"), err
	}

	today := time.Now().Format("2006-01-02")
	if student.SubExpDate == nil || *student.SubExpDate < today {
		student.PaymentStatus = "UNPAID"
	} else {
		student.PaymentStatus = "PAID"
	}

	if student.StudentClass != nil {
		for _, category := range validCategories {
			if strings.HasPrefix(category, *student.StudentClass) {
				student.Subjects = append(student.Subjects, category)
			}
		}
	}

	return createSuccessResponseData(student), nil
}

func handleSaveStudent(request events.LambdaFunctionURLRequest) (events.LambdaFunctionURLResponse, error) {
	var reqBody struct {
		Email        string `json:"email"`
		Name         string `json:"name"`
		PhoneNumber  string `json:"phoneNumber"`
		StudentClass string `json:"studentClass"`
	}

	if err := json.Unmarshal([]byte(request.Body), &reqBody); err != nil {
		return createErrorResponse(400, "Invalid JSON format in request body"), nil
	}

	if reqBody.Email == "" {
		return createErrorResponse(400, "Missing required field: 'email'"), nil
	}

	query := `INSERT INTO students (email, name, phone_number, student_class) 
			  VALUES ($1, $2, $3, $4) ON CONFLICT (email) DO NOTHING 
			  RETURNING id, email, name, student_class`
	
	var student Student
	err := getDB().QueryRow(query, strings.ToLower(reqBody.Email), 
		nullString(reqBody.Name), nullString(reqBody.PhoneNumber), 
		nullString(reqBody.StudentClass)).Scan(&student.ID, &student.Email, &student.Name, &student.StudentClass)
	
	if err == sql.ErrNoRows {
		return createErrorResponse(409, "Student already exists"), nil
	}
	if err != nil {
		return createErrorResponse(500, "Internal Server Error"), err
	}

	response := map[string]interface{}{
		"message": "Student created successfully",
		"student": student,
	}
	return events.LambdaFunctionURLResponse{
		StatusCode: 201,
		Headers:    getCORSHeaders(),
		Body:       marshal(response),
	}, nil
}