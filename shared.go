package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-lambda-go/events"
)

var (
	allowedEmails = []string{
		"rajeshc837@gmail.com",
		"rgvvarma009@gmail.com",
		"balamuralipati@gmail.com",
	}
	
	validCategories = []string{
		"CLS6-TELUGU", "CLS6-HINDI", "CLS6-ENGLISH", "CLS6-MATHS", "CLS6-SCIENCE", "CLS6-SOCIAL",
		"CLS7-TELUGU", "CLS7-HINDI", "CLS7-ENGLISH", "CLS7-MATHS", "CLS7-SCIENCE", "CLS7-SOCIAL",
		"CLS8-TELUGU", "CLS8-HINDI", "CLS8-ENGLISH", "CLS8-MATHS", "CLS8-SCIENCE", "CLS8-SOCIAL",
		"CLS9-TELUGU", "CLS9-HINDI", "CLS9-ENGLISH", "CLS9-MATHS", "CLS9-SCIENCE", "CLS9-SOCIAL",
		"CLS10-TELUGU", "CLS10-HINDI", "CLS10-ENGLISH", "CLS10-MATHS", "CLS10-SCIENCE", "CLS10-SOCIAL",
		"CLS10-BRIDGE", "CLS10-POLYTECHNIC", "CLS10-FORMULAS",
		"CLS11-MPC-PHYSICS", "CLS11-MPC-MATHS1A", "CLS11-MPC-MATHS1B", "CLS11-MPC-CHEMISTRY",
		"CLS11-MPC-EAMCET", "CLS11-MPC-JEEMAINS", "CLS11-MPC-JEEADV",
		"CLS12-MPC-PHYSICS", "CLS12-MPC-MATHS2A", "CLS12-MPC-MATHS2B", "CLS12-MPC-CHEMISTRY",
		"CLS12-MPC-EAMCET", "CLS12-MPC-JEEMAINS", "CLS12-MPC-JEEADV",
		"CLS11-BIPC-PHYSICS", "CLS11-BIPC-BOTANY", "CLS11-BIPC-ZOOLOGY", "CLS11-BIPC-CHEMISTRY",
		"CLS11-BIPC-EAPCET", "CLS11-BIPC-NEET",
		"CLS12-BIPC-PHYSICS", "CLS12-BIPC-BOTANY", "CLS12-BIPC-ZOOLOGY", "CLS12-BIPC-CHEMISTRY",
		"CLS12-BIPC-EAPCET", "CLS12-BIPC-NEET",
	}

	dateFilteredCategories = map[string]bool{
		"CLS11-MPC-EAMCET": true, "CLS11-MPC-JEEMAINS": true, "CLS11-MPC-JEEADV": true,
		"CLS12-MPC-EAMCET": true, "CLS12-MPC-JEEMAINS": true, "CLS12-MPC-JEEADV": true,
		"CLS11-BIPC-EAPCET": true, "CLS11-BIPC-NEET": true,
		"CLS12-BIPC-EAPCET": true, "CLS12-BIPC-NEET": true,
	}
)

type Student struct {
	ID           int       `json:"id"`
	Email        string    `json:"email"`
	Name         *string   `json:"name"`
	StudentClass *string   `json:"student_class"`
	PhoneNumber  *string   `json:"phone_number"`
	SubExpDate   *string   `json:"sub_exp_date"`
	UpdatedBy    *string   `json:"updated_by"`
	Amount       *float64  `json:"amount"`
	PaymentTime  *time.Time `json:"payment_time"`
	Role         *string   `json:"role"`
	PaymentStatus string   `json:"payment_status"`
	Subjects     []string  `json:"subjects"`
}

type Quiz struct {
	QuizName  string     `json:"quizName"`
	Duration  int        `json:"duration"`
	Category  string     `json:"category"`
	Questions []Question `json:"questions"`
}

func getUserEmail() string {
	return userEmailContext
}

func checkStudentPayment(email string) bool {
	var subExpDate *string
	query := `SELECT sub_exp_date FROM students WHERE LOWER(email) = LOWER($1)`
	err := getDB().QueryRow(query, email).Scan(&subExpDate)
	if err != nil {
		return false
	}

	today := time.Now().Format("2006-01-02")
	return subExpDate != nil && *subExpDate >= today
}

func createSuccessResponseData(data interface{}) events.LambdaFunctionURLResponse {
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers:    getCORSHeaders(),
		Body:       marshal(data),
	}
}

func createSuccessResponse(message string) events.LambdaFunctionURLResponse {
	return events.LambdaFunctionURLResponse{
		StatusCode: 200,
		Headers:    getCORSHeaders(),
		Body:       fmt.Sprintf(`{"message":"%s"}`, message),
	}
}

func marshal(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}