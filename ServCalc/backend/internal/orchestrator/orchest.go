package main

import (
	agents "calc/backend/internal/agent"
	agentrpc "calc/backend/internal/proto/calc_agent"
	orchest "calc/backend/internal/proto/orchest"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/sessions"
	_ "github.com/mattn/go-sqlite3"
	"google.golang.org/grpc"
)

type orchestratorServer struct {
	orchest.UnimplementedOrchestratorServer
}

var db *sql.DB

var store = sessions.NewCookieStore([]byte("secret-key"))

var op1Int int
var op2Int int
var op3Int int
var op4Int int
var op5Int int

type Result struct {
	Result string `json:"result"`
}

type Agent struct {
	ID     int
	Port   int
	Status string
	User   string
}

type Expression struct {
	ID         int
	Expression string
	Result     int
	Status     string
}

type User struct {
	Name     string `json:"login"`
	Password string `json:"password"`
}

func getOrDefault(value interface{}, defaultValue string) string {
	if value != nil {
		if str, ok := value.(string); ok {
			return str
		}
	}
	return defaultValue
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	// Отображаем страницу агентов с информацией из базы данных
	tmpl := template.Must(template.ParseFiles("frontend/regAndLog/login.html"))

	// Убедитесь, что вызов WriteHeader делается только один раз
	w.WriteHeader(http.StatusOK)

	if err := tmpl.Execute(w, nil); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error executing template: %v", err)
	}
}

func loginCheckHandler(w http.ResponseWriter, r *http.Request) {
	// Начинаем транзакцию
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error beginning transaction: %v", err)
		return
	}
	defer tx.Rollback() // Откатываем транзакцию в случае возникновения ошибки

	// Получаем данные из формы
	login := r.FormValue("login")
	password := r.FormValue("password")

	// Получаем куку с именем "token" из запроса
	cookie, err1 := r.Cookie("token")
	if err1 == nil && cookie != nil && cookie.Value != "" {
		user, err := getCookieToken(r)
		if err != nil {
			log.Printf("error of %v", err)
		}
		if login == user {
			// Отправляем сообщение об ошибке, если токен уже установлен
			errorMessage := map[string]string{"error": "Вы уже в системе!"}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(errorMessage)
			return
		}
	}

	// Получаем хеш пароля из базы данных
	var passwordHash string
	err = tx.QueryRow("SELECT password FROM Users WHERE Name = ?", login).Scan(&passwordHash)
	if err != nil {
		errorMessage := map[string]string{"error": "Неправильный логин! Может, попробуйте зарегистрироваться?"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(errorMessage)
		return
	}

	// Проверяем совпадение паролей
	if password != passwordHash {
		errorMessage := map[string]string{"error": "Неправильный пароль!"}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(errorMessage)
		return
	}

	// Заканчиваем транзакцию, фиксируя изменения
	if err := tx.Commit(); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error committing transaction: %v", err)
		return
	}

	session, err := store.Get(r, login)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	session.Save(r, w)

	// Генерируем JWT токен
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"name": login,
		"nbf":  now.Unix(),
		"exp":  now.Add(5 * time.Minute).Unix(),
		"iat":  now.Unix(),
	})

	tokenString, err := token.SignedString([]byte(login))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Формируем ответ с токеном в формате JSON
	response := map[string]string{"token": tokenString}

	// Устанавливаем токен в куки
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    tokenString,
		HttpOnly: true, // Чтобы кука была доступна только для HTTP запросов, а не JavaScript
	})

	// Устанавливаем заголовок Content-Type и отправляем ответ
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func registHandler(w http.ResponseWriter, r *http.Request) {
	// Отображаем страницу агентов с информацией из базы данных
	tmpl := template.Must(template.ParseFiles("frontend/regAndLog/register.html"))

	// Убедитесь, что вызов WriteHeader делается только один раз
	w.WriteHeader(http.StatusOK)

	if err := tmpl.Execute(w, nil); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error executing template: %v", err)
	}
}

func registerCheckHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Начинаем транзакцию
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error beginning transaction: %v", err)
		return
	}
	defer tx.Rollback() // Откатываем транзакцию в случае возникновения ошибки
	// Получаем данные из формы
	login := r.FormValue("login")
	password := r.FormValue("password")

	count := 0
	err = tx.QueryRow("SELECT COUNT(*) FROM Users WHERE Name = ?", login).Scan(&count)
	if err != nil {
		log.Print(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if count > 0 {
		http.Error(w, "User already exists", http.StatusBadRequest)
		return
	}

	// Вставляем нового пользователя в базу данных
	_, err = tx.Exec("INSERT INTO Users (Name, password) VALUES (?, ?)", login, password)
	if err != nil {
		log.Print(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Заканчиваем транзакцию, фиксируя изменения
	if err := tx.Commit(); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error committing transaction: %v", err)
		return
	}

	// Генерируем JWT токен
	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"name": login,
		"nbf":  now.Unix(),
		"exp":  now.Add(5 * time.Minute).Unix(),
		"iat":  now.Unix(),
	})

	session, err := store.Get(r, login)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	session.Save(r, w)

	tokenString, err := token.SignedString([]byte(login))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Формируем ответ с токеном в формате JSON
	response := map[string]string{"token": tokenString}

	// Устанавливаем токен в куки
	http.SetCookie(w, &http.Cookie{
		Name:     "token",
		Value:    tokenString,
		HttpOnly: true, // Чтобы кука была доступна только для HTTP запросов, а не JavaScript
	})

	// Устанавливаем заголовок Content-Type и отправляем ответ
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func orchestrateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Получаем куку с именем "token" из запроса
	user, err := getCookieToken(r)
	if err != nil {
		// Обработка ошибки, если кука не найдена или не может быть прочитана
		log.Print(err.Error())
	}

	var isauth bool
	var session *sessions.Session

	// Проверяем, есть ли имя пользователя
	if user != "" {
		// Если есть, получаем сессию по имени пользователя
		session, err = store.Get(r, user)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		isauth = true
	} else {
		// Если нет, создаем новую сессию без имени пользователя
		session, err = store.New(r, "session-name")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		isauth = false
	}

	// Получаем значения из сессии или используем дефолтные значения
	expression := getOrDefault(session.Values["expression"], "")
	addition := getOrDefault(session.Values["addition"], "200")
	subtraction := getOrDefault(session.Values["subtraction"], "200")
	multiplication := getOrDefault(session.Values["multiplication"], "200")
	division := getOrDefault(session.Values["division"], "200")
	exponent := getOrDefault(session.Values["exponent"], "200")

	// Преобразование строковых значений в целые числа
	op1Int, err = strconv.Atoi(addition)
	if err != nil {
		log.Printf("the session value(addition) is not int: %v\n", err)
	}
	op2Int, err = strconv.Atoi(subtraction)
	if err != nil {
		log.Printf("the session value(addition) is not int: %v\n", err)
	}
	op3Int, err = strconv.Atoi(multiplication)
	if err != nil {
		log.Printf("the session value(addition) is not int: %v\n", err)
	}
	op4Int, err = strconv.Atoi(division)
	if err != nil {
		log.Printf("the session value(addition) is not int: %v\n", err)
	}
	op5Int, err = strconv.Atoi(exponent)
	if err != nil {
		log.Printf("the session value(addition) is not int: %v\n", err)
	}

	expressions, err := getExpressions(user)
	if err != nil {
		log.Printf("вы не авторизованы")
	}

	// Отображаем страницу агентов с информацией из базы данных
	tmpl := template.Must(template.ParseFiles("frontend/agentsAndmain/index.html"))
	data := struct {
		Expression      string
		Addition        string
		Subtraction     string
		Multiplication  string
		Division        string
		Exponent        string
		Expressions     []Expression
		IsAuthenticated bool
	}{
		Expression:      expression,
		Addition:        addition,
		Subtraction:     subtraction,
		Multiplication:  multiplication,
		Division:        division,
		Exponent:        exponent,
		Expressions:     expressions,
		IsAuthenticated: isauth,
	}

	// Убедитесь, что вызов WriteHeader делается только один раз
	w.WriteHeader(http.StatusOK)

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error executing template: %v", err)
	}
}

func getCookieToken(r *http.Request) (string, error) {
	// Получаем куку с именем "token" из запроса
	cookie, err := r.Cookie("token")
	if err != nil {
		// Обработка ошибки, если кука не найдена или не может быть прочитана
		return "", errors.New("токен авторизации не найден, либо он повреждён")
	}

	// Извлекаем значение токена из куки
	tokenString := cookie.Value

	// Парсим и проверяем токен
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// // Извлекаем имя пользователя из токена
		user := token.Claims.(jwt.MapClaims)["name"].(string)
		// // Используем логин пользователя для создания ключа
		return []byte(user), nil
	})
	if err != nil && !token.Valid {
		// Если произошла ошибка при декодировании токена или токен невалиден
		return "", errors.New("невалидный токен")
	}

	// Теперь вы можете использовать имя пользователя, например, для аутентификации или других действий
	user := token.Claims.(jwt.MapClaims)["name"].(string)
	return user, nil
}

// Обработчик для вычисления выражения
func calcHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	expr := r.FormValue("expression")
	if expr == "" {
		http.Error(w, "Требуется выражение", http.StatusBadRequest)
		return
	}
	add, err := strconv.Atoi(r.FormValue("addition"))
	if err != nil {
		http.Error(w, "Невалидная скорость сложения", http.StatusBadRequest)
		return
	}
	subt, err := strconv.Atoi(r.FormValue("subtraction"))
	if err != nil {
		http.Error(w, "Невалидная скорость вычитания", http.StatusBadRequest)
		return
	}
	multip, err := strconv.Atoi(r.FormValue("multiplication"))
	if err != nil {
		http.Error(w, "Невалидная скорость умножения", http.StatusBadRequest)
		return
	}
	div, err := strconv.Atoi(r.FormValue("division"))
	if err != nil {
		http.Error(w, "Невалидная скорость деления", http.StatusBadRequest)
		return
	}
	exp, err := strconv.Atoi(r.FormValue("exponent"))
	if err != nil {
		http.Error(w, "Невалидная скорость степени", http.StatusBadRequest)
		return
	}
	var notval bool

	// Получаем куку с именем "token" из запроса
	cookie, err := r.Cookie("token")
	if err != nil {
		// Обработка ошибки, если кука не найдена или не может быть прочитана
		http.Error(w, "Токен авторизации для вычислений не найден", http.StatusUnauthorized)
		return
	}

	// Извлекаем значение токена из куки
	tokenString := cookie.Value

	// Парсим и проверяем токен
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		// Извлекаем имя пользователя из токена
		user := token.Claims.(jwt.MapClaims)["name"].(string)
		// Используем логин пользователя для создания ключа
		return []byte(user), nil
	})
	if err != nil || !token.Valid {
		// Если произошла ошибка при декодировании токена или токен невалиден
		http.Error(w, "Невалидный токен", http.StatusUnauthorized)
		return
	}

	// Теперь вы можете использовать имя пользователя, например, для аутентификации или других действий
	user := token.Claims.(jwt.MapClaims)["name"].(string)

	session, _ := store.Get(r, user)

	if user == "" {
		// Формируем ответ с токеном в формате JSON
		response := map[string]string{"error": "Токен должен быть обязательным!"}
		// Устанавливаем заголовок Content-Type и отправляем ответ
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Получаем значения из POST-запроса
	session.Values["expression"] = expr
	session.Values["addition"] = r.FormValue("addition")
	session.Values["subtraction"] = r.FormValue("subtraction")
	session.Values["multiplication"] = r.FormValue("multiplication")
	session.Values["division"] = r.FormValue("division")
	session.Values["exponent"] = r.FormValue("exponent")
	session.Save(r, w)

	// Проверяем выражение
	if !isValidExpression(expr) {
		http.Error(w, "Невалидное выражение", http.StatusBadRequest)
		return
	}

	// Если валидно, вычисляем
	if !notval {
		// вычисляем
		result, err := HandleCalculateRequest(expr, user, add, subt, multip, div, exp)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Обертываем результат в JSON и отправляем клиенту
		response := Result{Result: strconv.Itoa(result)}
		jsonResponse, err := json.Marshal(response)
		if err != nil {
			http.Error(w, "Error encoding JSON", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(jsonResponse)
	} else {
		// Ну если нет...
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		errorMessage := map[string]string{"error": "Невалидное выражение"}
		json.NewEncoder(w).Encode(errorMessage)
		return
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

// Реализация метода Ping
func (s orchestratorServer) Ping(ctx context.Context, req *orchest.PingRequest) (*orchest.PingResponse, error) {
	// Получаем ID агента из запроса
	agentID := req.GetAgentId()
	user := req.GetUser()

	// Логируем информацию о пинге от агента
	log.Printf("Получен пинг от агента %s пользователя %s", agentID, user)

	// Обновляем статус агента в базе данных
	if err := updateAgentStatus(agentID, "alive"); err != nil {
		log.Printf("Error updating agent status: %v", err)
		return nil, err
	}

	err := checkFreeExpressions(user)
	if err != nil {
		log.Fatal(err)
	}
	// Возвращаем успешный ответ
	return &orchest.PingResponse{Message: "Пинг успешно принят"}, nil
}

func checkFreeExpressions(user string) error { // Начинаем транзакцию
	tx, err := db.Begin()
	if err != nil {
		log.Printf("Error beginning transaction: %v", err)
		return err
	}
	defer tx.Rollback() // Откатываем транзакцию в случае возникновения ошибки

	rows, err := tx.Query("SELECT expression, status FROM expressions WHERE user = $1", user)
	if err != nil {
		return err
	}

	var express, status string
	for rows.Next() {

		if err := rows.Scan(&express, &status); err != nil {
			return err
		}

		if status == "pending" {
			break
		}
	}
	rows.Close()

	// Заканчиваем транзакцию, фиксируя изменения
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
		return err
	}

	_, err = HandleCalculateRequest(express, user, op1Int, op2Int, op3Int, op4Int, op5Int)
	if err != nil {
		log.Printf("Error while evaluate: %v", err)
	}

	return nil
}

// Обработчик для отображения информации об агентах
func agentsHandler(w http.ResponseWriter, r *http.Request) {
	// Начинаем транзакцию
	tx, err := db.Begin()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error beginning transaction: %v", err)
		return
	}
	defer tx.Rollback() // Откатываем транзакцию в случае возникновения ошибки

	type AgentInfo struct {
		ID         string
		Status     string
		LastActive time.Time
	}

	user, err := getCookieToken(r)
	if err != nil {
		log.Printf("Internal Server Error: %v", err)
	}

	var session *sessions.Session

	// Проверяем, есть ли имя пользователя
	if user != "" {
		// Если есть, получаем сессию по имени пользователя
		session, err = store.Get(r, user)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		// Если нет, создаем новую сессию без имени пользователя
		session, err = store.New(r, "session-name")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	// Выбираем информацию об агентах из базы данных
	rows, err := tx.Query("SELECT id, status, last_ping FROM agents WHERE user = ?", user)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error querying agents: %v", err)
		return
	}
	defer rows.Close()

	// Получаем текущее значение таймаута из сессии
	timeoutStr := getOrDefault(session.Values["timeout"], "10")

	timeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		http.Error(w, "timeout input must be integer", http.StatusInternalServerError)
		return
	}

	// Проверяем, было ли передано новое значение таймаута через параметр запроса
	if timeoutQuery := r.URL.Query().Get("timeout"); timeoutQuery != "" {
		timeoutStr = timeoutQuery
	}

	// Сохраняем новое значение таймаута в сессии
	session.Values["timeout"] = timeoutStr
	session.Save(r, w)

	var agents []AgentInfo
	// Получаем текущее время
	currentTime := time.Now()
	for rows.Next() {
		var agent AgentInfo
		if err := rows.Scan(&agent.ID, &agent.Status, &agent.LastActive); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			log.Printf("Error scanning agent row: %v", err)
			return
		}

		timeo := time.Duration(timeout) * time.Second
		// Если время последнего пинга превышает таймаут, помечаем агента как мертвого
		if currentTime.Sub(agent.LastActive) > timeo && agent.Status != "dead" {
			// Подготавливаем запрос UPDATE
			stmt, err := tx.Prepare("UPDATE agents SET status = ?, last_ping = ? WHERE id = ?")
			if err != nil {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				log.Printf("Error preparing update statement: %v", err)
				return
			}
			defer stmt.Close()

			// Выполняем запрос UPDATE
			_, err = stmt.Exec("dead", time.Now(), agent.ID)
			if err != nil {
				log.Printf("Error updating agent status: %v", err)
			} else {
				log.Print("Successfully updated agent status")
			}
		}
		agents = append(agents, agent)
	}

	// Заканчиваем транзакцию, фиксируя изменения
	if err := tx.Commit(); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error committing transaction: %v", err)
		return
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error iterating over agent rows: %v", err)
		return
	}

	// Отображаем страницу агентов с информацией из базы данных
	tmpl := template.Must(template.ParseFiles("frontend/agentsAndmain/agents.html"))
	data := struct {
		TimeoutStr string
		Agents     []AgentInfo
	}{
		TimeoutStr: timeoutStr,
		Agents:     agents,
	}

	// Убедитесь, что вызов WriteHeader делается только один раз
	w.WriteHeader(http.StatusOK)

	if err := tmpl.Execute(w, data); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error executing template: %v", err)
	}
}

func agentsCreater(w http.ResponseWriter, r *http.Request) {
	// Проверяем метод запроса
	if r.Method != "POST" {
		http.Error(w, "Метод не поддерживается", http.StatusMethodNotAllowed)
		return
	}

	user, err := getCookieToken(r)
	if err != nil {
		log.Printf("Internal Server Error: %v", err)
	}
	if user == "" {
		// Формируем ответ с токеном в формате JSON
		response := map[string]string{"error": "Токен должен быть обязательным!"}
		// Устанавливаем заголовок Content-Type и отправляем ответ
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	agents.StartAgent(user)

	// Формируем ответ с токеном в формате JSON
	response := map[string]string{"success": "Создан/Запущен агент."}
	log.Print(response)

	// Устанавливаем заголовок Content-Type и отправляем ответ
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func getExpressions(user string) ([]Expression, error) {
	if user == "" {
		return nil, fmt.Errorf("вы должны быть авторизованы")
	}
	// Начинаем транзакцию
	tx, err := db.Begin()
	if err != nil {
		log.Printf("Error beginning transaction: %v", err)
		return nil, err
	}
	defer tx.Rollback() // Откатываем транзакцию в случае возникновения ошибки

	rows, err := tx.Query("SELECT id, expression, result, status FROM expressions WHERE user = $1", user)
	if err != nil {
		return nil, fmt.Errorf("ошибка выбора таблицы из базы данных: %v", err)
	}
	defer rows.Close()

	var expressions []Expression
	for rows.Next() {
		var exp Expression
		var result sql.NullInt64
		if err := rows.Scan(&exp.ID, &exp.Expression, &result, &exp.Status); err != nil {
			return nil, fmt.Errorf("ошибка при сканировании строк: %v", err)
		}

		// Если значение Result пустое, заменяем его на 0
		if result.Valid {
			exp.Result = int(result.Int64)
		} else if !result.Valid {
			exp.Result = 0 // Или любое другое значение по умолчанию
		}

		expressions = append(expressions, exp)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ошибка при сканировании строк: %v", err)
	}

	// Заканчиваем транзакцию, фиксируя изменения
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
		return nil, err
	}

	return expressions, nil
}

func HandleCalculateRequest(expression, user string, op1, op2, op3, op4, op5 int) (int, error) {
	// Записываем выражение в базу данных
	expressionID, isInSQL, isResultNoExist, err := saveExpression(expression, user)
	if err != nil {
		return 0, err
	}
	if isInSQL && !isResultNoExist {
		resultReady, err := getOneExpression(expression, expressionID)
		if err != nil {
			return 0, err
		}

		return resultReady, nil
	}

	// Отправляем выражение агенту для вычисления
	result, err := computeExpression(expression, user, expressionID, op1, op2, op3, op4, op5)
	if err != nil {
		return 0, err
	}

	// Обновляем результат вычисления в базе данных
	err = updateExpressionResult(expressionID, result, "success")
	if err != nil {
		return 0, err
	}

	return result, nil
}

func getOneExpression(expr string, exprID int) (int, error) {
	// Начинаем транзакцию
	tx, err := db.Begin()
	if err != nil {
		log.Printf("Error beginning transaction: %v", err)
		return 0, err
	}
	defer tx.Rollback() // Откатываем транзакцию в случае возникновения ошибки

	// Проверяем, нет ли уже результата в базе данных
	rows, err := tx.Query("SELECT id, expression, result FROM expressions WHERE expression = ?", expr)
	if err != nil {
		return 0, fmt.Errorf("database error: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var expression string
		var result int

		if err := rows.Scan(&id, &expression, &result); err != nil {
			return 0, fmt.Errorf("error scanning row: %v ", err)
		}

		if id == exprID && expression == expr {
			return result, nil
		}
	}
	// Заканчиваем транзакцию, фиксируя изменения
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
		return 0, err
	}

	return 0, nil
}

func saveExpression(expression, user string) (int, bool, bool, error) {
	// Начинаем транзакцию
	tx, err := db.Begin()
	if err != nil {
		log.Printf("Error beginning transaction: %v", err)
		return 0, false, false, err
	}
	defer tx.Rollback() // Откатываем транзакцию в случае возникновения ошибки

	// Проверяем, нет ли уже результата в базе данных
	rows, err := tx.Query("SELECT id, expression, result, status FROM expressions WHERE expression = ? AND user = ?", expression, user)
	if err != nil {
		return 0, false, false, fmt.Errorf("database error: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var express, status string
		var result sql.NullInt64

		if err := rows.Scan(&id, &express, &result, &status); err != nil {
			return 0, false, false, fmt.Errorf("error scanning row: %v ", err)
		}

		if status == "success" && express == expression {
			return id, true, false, nil
		} else if status == "pending" && express == expression {
			return id, true, true, nil
		}
	}

	// Пишем выражение в базу данных и возвращаем его ID
	res, err := tx.Exec("INSERT INTO expressions (expression, status, user) VALUES (?, ?, ?)", expression, "pending", user)
	if err != nil {
		return 0, false, false, err
	}
	expressionID, err := res.LastInsertId()
	if err != nil {
		return 0, false, false, err
	}

	// Заканчиваем транзакцию, фиксируя изменения
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
		return 0, false, false, err
	}

	return int(expressionID), false, true, nil
}

func getAgentsFromDB() ([]Agent, error) {
	// Начинаем транзакцию
	tx, err := db.Begin()
	if err != nil {
		log.Printf("Error beginning transaction: %v", err)
		return nil, err
	}
	defer tx.Rollback() // Откатываем транзакцию в случае возникновения ошибки

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

func computeExpression(expression, user string, id, op1, op2, op3, op4, op5 int) (int, error) {
	agents, err := getAgentsFromDB()
	if err != nil {
		log.Fatalf("Error getting agents from database: %v", err)
	}

	port := 8081
	found := false

	// Перебираем список агентов
	for _, agent := range agents {
		// Проверяем статус агента
		if agent.Status == "alive" && agent.User == user {
			port = agent.Port
			found = true
		}
	}
	if !found {
		return 0, fmt.Errorf("не найдено свободных агентов")
	}

	// Создаем клиент gRPC для подключения к агенту
	conn, err := grpc.Dial(fmt.Sprintf("localhost:%d", port), grpc.WithInsecure())
	if err != nil {
		log.Fatalf("ошибка при наборе соединения с агентом: %v", err)
	}
	defer conn.Close()
	client := agentrpc.NewAgentClient(conn)

	// Создаем объект ExpressionRequest с выражением и временем выполнения операций
	req := &agentrpc.ExpressionRequest{
		Expression:     expression,
		Addition:       int64(op1),
		Subtraction:    int64(op2),
		Multiplication: int64(op3),
		Division:       int64(op4),
		Exponent:       int64(op5),
	}

	// Вызываем метод CalculateExpression на агенте
	resp, err := client.CalculateExpression(context.Background(), req)
	if err != nil {
		log.Fatalf("ошибка при вызове метода CalculateExpression: %v", err)
	}

	// Обновляем базу данных
	err = updateExpressionResult(id, 0, "processing")
	if err != nil {
		return 0, fmt.Errorf("ошибка обновления базы данных: %v", err)
	}

	result, err := strconv.Atoi(resp.Result)
	if err != nil {
		return 0, fmt.Errorf("ошибка преобразования результата в число: %v", err)
	}

	return result, nil
}

func updateExpressionResult(expressionID int, result int, status string) error {
	// Начинаем транзакцию
	tx, err := db.Begin()
	if err != nil {
		log.Printf("Error beginning transaction: %v", err)
		return err
	}
	defer tx.Rollback() // Откатываем транзакцию в случае возникновения ошибки

	// Обновляем результат вычисления в базе данных
	_, err = tx.Exec("UPDATE expressions SET result=?, status=? WHERE id=?", result, status, expressionID)

	// Заканчиваем транзакцию, фиксируя изменения
	if err := tx.Commit(); err != nil {
		log.Printf("Error committing transaction: %v", err)
		return err
	}

	return err
}

func isValidExpression(expression string) bool {
	// Проверяем наличие неизвестных символов
	unknownSymbols := regexp.MustCompile(`[^0-9()+\-*/^.]`).FindAllString(expression, -1)
	if len(unknownSymbols) > 0 {
		fmt.Println("Выражение содержит неизвестные символы:", unknownSymbols)
		return false
	}

	// Проверяем количество открывающих и закрывающих скобок
	if strings.Count(expression, "(") != strings.Count(expression, ")") {
		fmt.Println("Неправильное количество скобок")
		return false
	}

	// Проверяем валидность выражения с помощью регулярного выражения
	pattern := `-?\d+(\.\d+)?`
	numbers := regexp.MustCompile(pattern).FindAllString(expression, -1)
	for _, num := range numbers {
		matched, err := regexp.MatchString("^"+pattern+"$", num)
		if err != nil {
			fmt.Println("Ошибка при выполнении регулярного выражения:", err)
			return false
		}
		if !matched {
			fmt.Println("Число", num, "невалидно")
			return false
		}
	}

	return true
}

func initDB() {
	var err error
	db, err = sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
}

func main() {
	var wg sync.WaitGroup
	// Создаем маршрутизатор HTTP запросов
	mux := http.NewServeMux()

	// Подключаемся к базе данных
	initDB()
	defer db.Close()

	// Создаем таблицу для хранения выражений
	_, err := db.Exec("CREATE TABLE IF NOT EXISTS expressions (id INTEGER PRIMARY KEY AUTOINCREMENT, expression TEXT, result INTEGER, status TEXT, user TEXT);")
	if err != nil {
		log.Fatal("Error creating table:", err)
	}

	// Создаем таблицу для хранения пользователей
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS Users (id INTEGER PRIMARY KEY AUTOINCREMENT, Name TEXT, password TEXT);")
	if err != nil {
		log.Fatal("Error creating table:", err)
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS agents (id INTEGER PRIMARY KEY, hostname TEXT NOT NULL, port TEXT NOT NULL, last_ping TIMESTAMP DEFAULT CURRENT_TIMESTAMP, user TEXT);")
	if err != nil {
		log.Fatal("Error creating table:", err)
	}

	mux.HandleFunc("/", orchestrateHandler)
	mux.HandleFunc("/login", loginHandler)
	mux.HandleFunc("/loginCheck", loginCheckHandler)
	mux.HandleFunc("/register", registHandler)
	mux.HandleFunc("/registerCheck", registerCheckHandler)
	mux.HandleFunc("/calculate", calcHandler)
	mux.HandleFunc("/agents", agentsHandler)
	mux.HandleFunc("/createAgents", agentsCreater)

	// Создаем gRPC сервер
	grpcServer := grpc.NewServer()

	// Увеличиваем счетчик WaitGroup для каждой горутины
	wg.Add(2)

	// Запуск gRPC сервера в отдельной горутине
	go func() {
		defer wg.Done()
		// Создаем TCP слушатель для gRPC сервера
		lis, err := net.Listen("tcp", ":8079")
		if err != nil {
			log.Fatalf("failed to listen: %v", err)
		}
		defer lis.Close()
		server := orchestratorServer{}
		// Регистрация вашего сервера сгенерированным кодом gRPC
		orchest.RegisterOrchestratorServer(grpcServer, server)

		// Запускаем gRPC сервер
		log.Printf("gRPC server listening on port %s", lis.Addr().String())
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve gRPC: %v", err)
		}
	}()

	// Запуск HTTP сервера
	go func() {
		defer wg.Done()
		log.Printf("HTTP server listening on port :8080")
		log.Fatal(http.ListenAndServe(":8080", mux))
	}()

	// Ожидание завершения работы всех горутин
	wg.Wait()
}
