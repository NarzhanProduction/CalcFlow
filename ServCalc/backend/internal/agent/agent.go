package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"
	"unicode"
)

type Result struct {
	Result string `json:"result"`
}

func main() {

	http.HandleFunc("/", HandleExpression)
	go func() {
		for {
			fmt.Println("connected")
			time.Sleep(1 * time.Minute)
		}
	}()
	http.ListenAndServe(":8081", nil)
}

func sendPing() error {
	// Формируем JSON с данными пинга
	pingData := struct {
		AgentID int `json:"agent_id"`
	}{
		AgentID: 1, // Замените на реальный идентификатор агента
	}
	requestBody, err := json.Marshal(pingData)
	if err != nil {
		return err
	}

	// Отправляем POST-запрос оркестратору
	resp, err := http.Post("http://localhost:8080", "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Проверяем статус ответа
	if resp.StatusCode != http.StatusOK {
		return errors.New("unexpected status code")
	}

	return nil
}

var available bool

func IsAvailable() bool {
	return available
}

// HandleExpression принимает выражение от оркестратора, вычисляет его и отправляет результат обратно.
func HandleExpression(w http.ResponseWriter, r *http.Request) {
	// Декодируем JSON с выражением
	var data map[string]string
	err := json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		http.Error(w, "неверный формат запроса", http.StatusBadRequest)
		return
	}

	expression, ok := data["expression"]
	if !ok {
		http.Error(w, "отсутствует выражение в запросе", http.StatusBadRequest)
		return
	}

	// Вычисляем выражение
	result, err := evaluateExpression(expression)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Формируем JSON с результатом

	response := Result{Result: fmt.Sprint(int(result))}
	jsonResponse, err := json.Marshal(response)
	if err != nil {
		http.Error(w, "Ошибка кодирования JSON", http.StatusInternalServerError)
		return
	}

	// Отправка JSON-ответа оркестратору
	w.Header().Set("Content-Type", "application/json")
	w.Write(jsonResponse)
	fmt.Print("Запрос успешно отправлен оркестратору: ", string(jsonResponse))
}

func evaluateExpression(expression string) (float64, error) {
	available = false
	tokens := tokenize(expression)
	postfix := infixToPostfix(tokens)
	result, err := evaluatePostfix(postfix)
	available = true
	return result, err
}

func tokenize(expression string) []string {
	var tokens []string
	var current string
	for _, char := range expression {
		if unicode.IsDigit(char) || char == '.' {
			current += string(char)
		} else if char == '+' || char == '-' || char == '*' || char == '/' || char == '(' || char == ')' || char == '^' {
			if current != "" {
				tokens = append(tokens, current)
				current = ""
			}
			tokens = append(tokens, string(char))
		}
	}
	if current != "" {
		tokens = append(tokens, current)
	}
	return tokens
}

func infixToPostfix(infix []string) []string {
	var postfix []string
	var operators []string
	for _, token := range infix {
		if isNumber(token) {
			postfix = append(postfix, token)
		} else if token == "(" {
			operators = append(operators, token)
		} else if token == ")" {
			for len(operators) > 0 && operators[len(operators)-1] != "(" {
				postfix = append(postfix, operators[len(operators)-1])
				operators = operators[:len(operators)-1]
			}
			if len(operators) == 0 || operators[len(operators)-1] != "(" {
				panic("Неверное выражение: непарные скобки")
			}
			operators = operators[:len(operators)-1]
		} else {
			for len(operators) > 0 && precedence(token) <= precedence(operators[len(operators)-1]) {
				postfix = append(postfix, operators[len(operators)-1])
				operators = operators[:len(operators)-1]
				if token != "^" && token != "-" && token != "*" && token != "/" && token != "+" {
					panic("Неверное выражение: неизвестный оператор")
				}
			}
			operators = append(operators, token)
		}
	}
	for len(operators) > 0 {
		postfix = append(postfix, operators[len(operators)-1])
		operators = operators[:len(operators)-1]
	}
	return postfix
}

func evaluatePostfix(postfix []string) (float64, error) {
	var stack []float64
	for _, token := range postfix {
		if isNumber(token) {
			num, err := strconv.ParseFloat(token, 64)
			if err != nil {
				return 0, fmt.Errorf("ошибка преобразования числа: %v", err)
			}
			stack = append(stack, num)
		} else if token == "^" || token == "-" || token == "*" || token == "/" || token == "+" {
			if len(stack) < 2 {
				return 0, fmt.Errorf("недостаточно операндов для выполнения операции: %s", token)
			}
			operand2 := stack[len(stack)-1]
			operand1 := stack[len(stack)-2]
			stack = stack[:len(stack)-2]
			switch token {
			case "+":
				stack = append(stack, operand1+operand2)
			case "-":
				stack = append(stack, operand1-operand2)
			case "*":
				stack = append(stack, operand1*operand2)
			case "^":
				stack = append(stack, math.Pow(operand1, operand2))
			case "/":
				if operand2 == 0 {
					return 0, fmt.Errorf("деление на ноль")
				}
				stack = append(stack, operand1/operand2)
			}
		} else {
			return 0, fmt.Errorf("неподдерживаемые операнды")
		}
	}
	if len(stack) != 1 {
		return 0, fmt.Errorf("неверное количество операндов: %d", len(stack))
	}
	return stack[0], nil
}

func isNumber(str string) bool {
	_, err := strconv.ParseFloat(str, 64)
	return err == nil
}

func precedence(op string) int {
	switch op {
	case "+", "-":
		return 1
	case "*", "/":
		return 2
	case "^":
		return 3
	default:
		return 0
	}
}
