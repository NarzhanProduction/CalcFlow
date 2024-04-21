package agent

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"math"
	"net"
	"strconv"
	"time"
	"unicode"

	agentrpc "calc/backend/internal/proto/calc_agent"
	orchest "calc/backend/internal/proto/orchest"

	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type agentServer struct {
	// Встраиваем пустую реализацию UnimplementedAgentServer
	agentrpc.UnimplementedAgentServer
	id int
}

// Реализация метода CalculateExpression
func (s agentServer) CalculateExpression(ctx context.Context, req *agentrpc.ExpressionRequest) (*agentrpc.Result, error) {
	// Получаем значения выражения и времени выполнения операций из объекта req
	expression := req.GetExpression()
	addition := req.GetAddition()
	subtraction := req.GetSubtraction()
	multiplication := req.GetMultiplication()
	division := req.GetDivision()
	exponent := req.GetExponent()

	// Вызываем функцию для вычисления выражения с полученными значениями времени выполнения операций
	result, err := evaluateExpression(expression, fmt.Sprint(s.id), int(addition), int(subtraction), int(multiplication), int(division), int(exponent))
	if err != nil {
		return nil, err
	}

	// Создаем и возвращаем объект pb.ExpressionResponse с результатом вычисления
	return &agentrpc.Result{Result: fmt.Sprint(int(result))}, nil
}

var db *sql.DB

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
	agents, err := getAgentsFromDB()
	if err != nil {
		log.Fatalf("Error getting agents from database: %v", err)
	}
	// Перебираем список агентов
	for _, agent := range agents {
		// Проверяем статус агента
		if agent.Status == "dead" && agent.User == user {
			// Создание TCP соединения на указанном порту
			lis, err := net.Listen("tcp", fmt.Sprintf(":%d", agent.Port))
			if err != nil {
				log.Fatalf("failed to listen: %v", err)
			}
			// Создание нового сервера gRPC
			grpcServer := grpc.NewServer()
			server := agentServer{agentrpc.UnimplementedAgentServer{}, agent.ID}
			// Регистрация вашего сервера сгенерированным кодом gRPC
			agentrpc.RegisterAgentServer(grpcServer, server)
			// Если агент мертв, запускаем его сервер
			log.Print("starting agent...")
			orchestratorURL := "localhost:8079"
			// Функция для отправки пинга оркестратору
			go sendPing(orchestratorURL, fmt.Sprint(agent.ID), agent.User)
			// Запуск сервера
			if err := grpcServer.Serve(lis); err != nil {
				log.Fatalf("failed to serve: %v", err)
			}
		}
	}

	// Создаем нового агента и запускаем его сервер
	if len(agents) > 0 {
		initDB()
		defer db.Close()
		newAgentPort := agents[len(agents)-1].Port + 1
		newAgentID := agents[len(agents)-1].ID + 1
		// Создание TCP соединения на указанном порту
		lis, err := net.Listen("tcp", fmt.Sprintf(":%d", newAgentPort))
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
		// Создание нового сервера gRPC
		grpcServer := grpc.NewServer()
		server := agentServer{agentrpc.UnimplementedAgentServer{}, newAgentID}
		// Регистрация вашего сервера сгенерированным кодом gRPC
		agentrpc.RegisterAgentServer(grpcServer, server)
		// Если агент мертв, запускаем его сервер
		log.Print("starting agent...")
		orchestratorURL := "localhost:8079"
		// Функция для отправки пинга оркестратору
		go sendPing(orchestratorURL, fmt.Sprint(newAgentID), user)
		// Сохраняем агента в БД
		updateAgentsDB(newAgentID, newAgentPort, user)
		// Запуск сервера
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	} else {
		initDB()
		defer db.Close()
		// Обработка случая, когда список агентов пустой
		newAgentPort := 8081
		newAgentID := 1
		// Создание TCP соединения на указанном порту
		lis, err := net.Listen("tcp", fmt.Sprintf(":%d", newAgentPort))
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
		// Создание нового сервера gRPC
		grpcServer := grpc.NewServer()
		server := agentServer{agentrpc.UnimplementedAgentServer{}, newAgentID}
		// Регистрация вашего сервера сгенерированным кодом gRPC
		agentrpc.RegisterAgentServer(grpcServer, server)
		// Если агент мертв, запускаем его сервер
		log.Print("starting agent...")
		orchestratorURL := "localhost:8079"
		// Функция для отправки пинга оркестратору
		go sendPing(orchestratorURL, fmt.Sprint(newAgentID), user)
		// Сохраняем агента в БД
		updateAgentsDB(newAgentID, newAgentPort, user)
		// Запуск сервера
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
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

func sendPing(orchestURL, agentID, user string) {
	// Создаем таймер для повторения операции каждые 7 секунд
	ticker := time.NewTicker(7 * time.Second)
	defer ticker.Stop()

	// Бесконечный цикл для отправки пингов
	for range ticker.C {
		// Вызываем функцию отправки пинга
		err := sendPingOnce(orchestURL, agentID, user)
		if err != nil {
			// Обработка ошибки, если таковая имеется
			// Например, логирование или другие действия
			log.Printf("Ошибка при отправке пинга: %v", err)
		}
	}
}

// sendPingOnce отправляет один раз пинг оркестратору от агента
func sendPingOnce(orchestURL, agentID, user string) error {
	// Создаем клиент gRPC с небезопасными учетными данными
	creds := insecure.NewCredentials()
	// Создаем клиент gRPC
	conn, err := grpc.Dial(orchestURL, grpc.WithTransportCredentials(creds))
	if err != nil {
		return fmt.Errorf("ошибка при наборе соединения с оркестратором: %v", err)
	}
	defer conn.Close()
	client := orchest.NewOrchestratorClient(conn)

	// Создаем объект PingRequest с ID агента
	req := &orchest.PingRequest{
		AgentId: agentID,
		User:    user,
	}

	// Вызываем метод Ping на оркестраторе
	_, err = client.Ping(context.Background(), req)
	if err != nil {
		return fmt.Errorf("ошибка при вызове метода Ping на оркестраторе: %v", err)
	}

	return nil
}

// HandleExpression принимает выражение от оркестратора, вычисляет его и отправляет результат обратно.
func HandleExpression(id int, req *agentrpc.ExpressionRequest) (*agentrpc.Result, error) {
	// Создаем клиент gRPC с небезопасными учетными данными
	creds := insecure.NewCredentials()
	// Создаем клиент gRPC
	conn, err := grpc.Dial("localhost:8080", grpc.WithTransportCredentials(creds))
	if err != nil {
		log.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()
	client := agentrpc.NewAgentClient(conn)

	// Вызываем метод Calculate вашего gRPC сервиса
	response, err := client.CalculateExpression(context.Background(), req)
	if err != nil {
		log.Print("ошибка при вызове gRPC сервиса")
		return nil, err
	}

	return response, nil
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
