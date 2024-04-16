package agents

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"
	"unicode"

	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
)

var db *sql.DB

type Result struct {
	Result string `json:"result"`
}

type Agent struct {
	ID     int
	Port   int
	Status string
	User   string
}

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
}

func updateAgentStatus(agentID, status string) error {
	// Начинаем транзакцию
	tx, err := db.Begin()
	if err != nil {
		log.Printf("Error beginning transaction: %v", err)
		return err
	}
	defer tx.Rollback() // Откатываем транзакцию в случае возникновения ошибки

	// Выполняем запрос обновления в рамках транзакции
	_, err = tx.Exec("UPDATE agents SET status = ?, last_ping = ? WHERE id = ?", status, time.Now(), agentID)
	if err != nil {
		return err
	}

	// Заканчиваем транзакцию, фиксируя изменения
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
		return err
	}

	return nil
}

func getAgentsFromDB() ([]Agent, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, fmt.Errorf("error opening database: %v", err)
	}

	rows, err := tx.Query("SELECT id, port, status, user FROM agents")
	if err != nil {
		return nil, fmt.Errorf("error querying agents: %v", err)
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var agent Agent
		if err := rows.Scan(&agent.ID, &agent.Port, &agent.Status, &agent.User); err != nil {
			return nil, fmt.Errorf("error scanning agent row: %v", err)
		}
		agents = append(agents, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over agent rows: %v", err)
	}

	// Заканчиваем транзакцию, фиксируя изменения
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
		return nil, err
	}

	return agents, nil
}

func StartAgent(user string) {
	initDB()
	defer db.Close()
	router := mux.NewRouter()
	agents, err := getAgentsFromDB()
	if err != nil {
		log.Fatalf("Error getting agents from database: %v", err)
	}
	// Перебираем список агентов
	for _, agent := range agents {
		// Проверяем статус агента
		if agent.Status == "dead" && agent.User == user {
			// Если агент мертв, запускаем его сервер
			log.Print("starting agent...")
			orchestratorURL := "http://localhost:8080"
			router.HandleFunc("/", HandleExpression(agent.Port, agent.ID))
			// Функция для отправки пинга оркестратору
			go sendPing(orchestratorURL, fmt.Sprint(agent.ID), agent.User)
			// Запуск сервера
			http.ListenAndServe(fmt.Sprintf(":%d", agent.Port), router)
		}
	}

	// Создаем нового агента и запускаем его сервер
	if len(agents) > 0 {
		initDB()
		defer db.Close()
		newAgentPort := agents[len(agents)-1].Port + 1
		newAgentID := agents[len(agents)-1].ID + 1
		log.Print("starting agent...")
		// Если агент мертв, запускаем его сервер
		orchestratorURL := "http://localhost:8080"
		router.Handle("/", HandleExpression(newAgentPort, newAgentID))
		// Функция для отправки пинга оркестратору
		go sendPing(orchestratorURL, fmt.Sprint(newAgentID), user)
		// Сохраняем агента в БД
		updateAgentsDB(newAgentID, newAgentPort, user)
		// Запуск сервера
		http.ListenAndServe(fmt.Sprintf(":%d", newAgentPort), router)
	} else {
		initDB()
		defer db.Close()
		// Обработка случая, когда список агентов пустой
		newAgentPort := 8081
		newAgentID := 1
		orchestratorURL := "http://localhost:8080"
		log.Print("starting agent...")
		router.Handle("/", HandleExpression(newAgentPort, newAgentID))
		// Функция для отправки пинга оркестратору
		go sendPing(orchestratorURL, fmt.Sprint(newAgentID), user)
		// Сохраняем агента в БД
		updateAgentsDB(newAgentID, newAgentPort, user)
		// Запуск сервера
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", newAgentPort), router))
	}
}

func updateAgentsDB(id, port int, user string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	// Если запись с указанным ID не существует, вставляем новую запись
	_, err = tx.Exec("INSERT INTO agents (id, port, status, user) VALUES (?, ?, ?, ?)", id, port, "alive", user)
	if err != nil {
		return err
	}
	fmt.Printf("Inserted new agent: ID=%d, Port=%d\n", id, port)

	// Заканчиваем транзакцию, фиксируя изменения
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
		return err
	}

	return nil
}

func sendPing(orchestratorURL, agentID, user string) {
	for {
		// Отправляем GET запрос на оркестратор для отправки пинга
		req, err := http.NewRequest("GET", orchestratorURL+"/ping?id="+agentID, nil)
		if err != nil {
			log.Printf("Error sending ping: %v", err)
		}

		// Добавляем заголовок с никнеймом
		req.Header.Set("Authorization", "Bearer "+user)

		// Отправляем запрос
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("Error sending ping: %v", err)
			continue
		}

		// Закрываем тело ответа
		resp.Body.Close()

		// Ждем некоторое время перед отправкой следующего пинга
		time.Sleep(7 * time.Second)
	}
}

// HandleExpression принимает выражение от оркестратора, вычисляет его и отправляет результат обратно.
func HandleExpression(port, id int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		add, ok := data["op1"]
		if !ok {
			http.Error(w, "отсутствует оператор в запросе", http.StatusBadRequest)
			return
		}
		sub, ok := data["op2"]
		if !ok {
			http.Error(w, "отсутствует оператор в запросе", http.StatusBadRequest)
			return
		}
		mul, ok := data["op3"]
		if !ok {
			http.Error(w, "отсутствует оператор в запросе", http.StatusBadRequest)
			return
		}
		div, ok := data["op4"]
		if !ok {
			http.Error(w, "отсутствует оператор в запросе", http.StatusBadRequest)
			return
		}
		exp, ok := data["op5"]
		if !ok {
			http.Error(w, "отсутствует оператор в запросе", http.StatusBadRequest)
			return
		}

		op1, err := strconv.Atoi(add)
		if err != nil {
			http.Error(w, "оператор не является числом!", http.StatusBadRequest)
			return
		}

		op2, err := strconv.Atoi(sub)
		if err != nil {
			http.Error(w, "оператор не является числом!", http.StatusBadRequest)
			return
		}

		op3, err := strconv.Atoi(mul)
		if err != nil {
			http.Error(w, "оператор не является числом!", http.StatusBadRequest)
			return
		}

		op4, err := strconv.Atoi(div)
		if err != nil {
			http.Error(w, "оператор не является числом!", http.StatusBadRequest)
			return
		}

		op5, err := strconv.Atoi(exp)
		if err != nil {
			http.Error(w, "оператор не является числом!", http.StatusBadRequest)
			return
		}

		// Вычисляем выражение
		result, err := evaluateExpression(expression, fmt.Sprint(id), op1, op2, op3, op4, op5)
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
}

func evaluateExpression(expression, id string, op1, op2, op3, op4, op5 int) (float64, error) {
	updateAgentStatus(id, "busy")
	tokens := tokenize(expression)
	postfix := infixToPostfix(tokens)
	result, err := evaluatePostfix(postfix)
	//Cчитаем время
	total := Time(expression, op1, op2, op3, op4, op5)
	time.Sleep(time.Duration(total) * time.Millisecond)
	updateAgentStatus(id, "alive")
	return result, err
}

func Time(expr string, op1, op2, op3, op4, op5 int) int {
	totaltime := 0
	for _, token := range expr {
		switch {
		case token == '+':
			totaltime += op1
		case token == '-':
			totaltime += op2
		case token == '*':
			totaltime += op3
		case token == '/':
			totaltime += op4
		case token == '^':
			totaltime += op5
		}
	}
	return totaltime
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
