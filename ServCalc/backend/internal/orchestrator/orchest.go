package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/gorilla/sessions"
	_ "github.com/mattn/go-sqlite3"
)

var (
	store = sessions.NewCookieStore([]byte("secret-key"))
)

var op1Int int
var op2Int int
var op3Int int
var op4Int int
var op5Int int

type Result struct {
	Result string `json:"result"`
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
	loginHTML := `<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>Вход</title>
		<link rel="stylesheet" href="https://maxcdn.bootstrapcdn.com/bootstrap/4.5.2/css/bootstrap.min.css">
	</head>
	<body>
		<div class="container">
			<div class="row justify-content-center mt-5">
				<div class="col-md-6">
					<h1 class="text-center mb-4">Вход</h1>
					<form id="loginForm" action="/loginCheck" method="POST">
						<div class="form-group">
							<label for="login">Логин:</label>
							<input type="text" class="form-control" id="login" name="login" required>
						</div>
						<div class="form-group">
							<label for="password">Пароль:</label>
							<input type="password" class="form-control" id="password" name="password" required>
						</div>
						<button type="submit" class="btn btn-primary btn-block">Войти</button>
					</form>
					<div id="message" class="text-center mt-3"></div>
					<div class="text-center mt-3">
						<a href="/" class="btn btn-secondary">Назад</a>
					</div>
				</div>
			</div>
		</div>

		<script src="https://code.jquery.com/jquery-3.5.1.slim.min.js"></script>
		<script src="https://cdn.jsdelivr.net/npm/@popperjs/core@2.5.4/dist/umd/popper.min.js"></script>
		<script src="https://maxcdn.bootstrapcdn.com/bootstrap/4.5.2/js/bootstrap.min.js"></script>
	
		<script>
		document.getElementById("loginForm").addEventListener("submit", function(event) {
			event.preventDefault();
			var formData = new FormData(this);
			fetch("/loginCheck", {
				method: "POST",
				body: formData
			})
			.then(response => response.json())
			.then(data => {
				if (data.token) {
					document.getElementById("message").innerHTML = '<div class="alert alert-success" role="alert">Успешный вход! Токен: ' + data.token + '</div>';
				} else {
					document.getElementById("message").innerHTML = '<div class="alert alert-danger" role="alert">Ошибка входа: ' + data.error + '</div>';
				}
			})
			.catch(error => {
				console.error("Ошибка:", error);
				document.getElementById("message").innerHTML = '<div class="alert alert-danger" role="alert">Произошла ошибка при входе: ' + error + '</div>';
			});                    
		});		
		</script>
	</body>
	</html>`

	fmt.Fprint(w, loginHTML)
}

func loginCheckHandler(w http.ResponseWriter, r *http.Request) {
	// Получаем данные из формы
	login := r.FormValue("login")
	password := r.FormValue("password")

	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
	defer db.Close()

	// Получаем хеш пароля из базы данных
	var passwordHash string
	err = db.QueryRow("SELECT password FROM Users WHERE Name = ?", login).Scan(&passwordHash)
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

	// Получаем куку с именем "token" из запроса
	cookie, err1 := r.Cookie("token")
	if cookie.Value != "" {
		user, err := getCookieToken(r)
		if err != nil {
			log.Printf("error of %v", err)
		}
		if err1 == nil && login == user {
			// Отправляем сообщение об ошибке, если токен уже установлен
			errorMessage := map[string]string{"error": "Вы уже в системе!"}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(errorMessage)
			return
		}
	}

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
	registerHTML := `<!DOCTYPE html>
	<html lang="en">
	<head>
		<meta charset="UTF-8">
		<meta name="viewport" content="width=device-width, initial-scale=1.0">
		<title>Регистрация</title>
		<link rel="stylesheet" href="https://maxcdn.bootstrapcdn.com/bootstrap/4.5.2/css/bootstrap.min.css">
	</head>
	<body>
		<div class="container">
			<div class="row justify-content-center mt-5">
				<div class="col-md-6">
					<h1 class="text-center mb-4">Регистрация</h1>
					<form id="registerForm" action="/registerCheck" method="POST">
						<div class="form-group">
							<label for="login">Логин:</label>
							<input type="text" class="form-control" id="login" name="login" required>
						</div>
						<div class="form-group">
							<label for="password">Пароль:</label>
							<input type="password" class="form-control" id="password" name="password" required>
						</div>
						<button type="submit" class="btn btn-primary btn-block">Зарегистрироваться</button>
					</form>
					<div id="message" class="text-center mt-3"></div>
					<div class="text-center mt-3">
						<a href="/" class="btn btn-secondary">Назад</a>
					</div>
				</div>
			</div>
		</div>

		<script src="https://code.jquery.com/jquery-3.5.1.slim.min.js"></script>
		<script src="https://cdn.jsdelivr.net/npm/@popperjs/core@2.5.4/dist/umd/popper.min.js"></script>
		<script src="https://maxcdn.bootstrapcdn.com/bootstrap/4.5.2/js/bootstrap.min.js"></script>
	
		<script>
		document.getElementById("registerForm").addEventListener("submit", function(event) {
			event.preventDefault();
			var formData = new FormData(this);
			fetch("/registerCheck", {
				method: "POST",
				body: formData
			})
			.then(response => response.json())
			.then(data => {
				if (data.token) {
					document.getElementById("message").innerHTML = '<div class="alert alert-success" role="alert">Успешная регистрация!</div>';
				} else {
					document.getElementById("message").innerHTML = '<div class="alert alert-danger" role="alert">Ошибка регистрации: ' + data.error + '</div>';
				}
			})
			.catch(error => {
				console.error("Ошибка:", error);
				document.getElementById("message").innerHTML = '<div class="alert alert-danger" role="alert">Произошла ошибка при регистрации: ' + error + '</div>';
			});                    
		});		
		</script>
	</body>
	</html>`

	fmt.Fprint(w, registerHTML)
}
func registerCheckHandler(w http.ResponseWriter, r *http.Request) {
	// Получаем данные из формы
	login := r.FormValue("login")
	password := r.FormValue("password")

	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM Users WHERE Name = ?", login).Scan(&count)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if count > 0 {
		http.Error(w, "User already exists", http.StatusBadRequest)
		return
	}

	// Вставляем нового пользователя в базу данных
	_, err = db.Exec("INSERT INTO Users (Name, password) VALUES (?, ?)", login, password)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
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

	session, _ := store.Get(r, "session-name")

	// Получаем значения из сессии или используем дефолтные значения
	expression := getOrDefault(session.Values["expression"], "")
	addition := getOrDefault(session.Values["addition"], "200")
	subtraction := getOrDefault(session.Values["subtraction"], "200")
	multiplication := getOrDefault(session.Values["multiplication"], "200")
	division := getOrDefault(session.Values["division"], "200")
	exponent := getOrDefault(session.Values["exponent"], "200")

	// Преобразование строковых значений в целые числа
	var err error
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

	// Получаем куку с именем "token" из запроса
	user, err := getCookieToken(r)
	if err != nil {
		// Обработка ошибки, если кука не найдена или не может быть прочитана
		log.Print(err.Error())
	}

	expressions, err := getExpressions(user)
	if err != nil {
		log.Printf("вы не авторизованы")
	}

	html := `<!DOCTYPE html>
	<html>
	<head>
		<title>Арифметический калькулятор</title>
		<link rel="stylesheet" href="https://maxcdn.bootstrapcdn.com/bootstrap/4.5.2/css/bootstrap.min.css">
	</head>
	<body>
		<div class="container mt-5">
        	<div class="jumbotron">
            	<a class="btn btn-primary btn-lg" href="/agents" role="button">Агенты</a>
            	<a class="btn btn-primary btn-lg" href="/register" role="button">Регистрация</a>
            	<a class="btn btn-primary btn-lg" href="/login" role="button">Логин</a>
            	<h1 class="display-4">Арифметический калькулятор</h1>
            	<hr class="my-4">
        	</div>
			<h1>Арифметический калькулятор</h1>
			<form id="expressionForm">
				<div class="form-group">
					<label for="expression">Введите выражение:</label>
					<input type="text" class="form-control" id="expression" name="expression" value="` + expression + `"><br>
				</div>
				<div class="form-group">
					<label for="addition">Время выполнения сложения (в миллисекундах):</label>
					<input type="text" class="form-control" id="addition" name="addition" value="` + addition + `"><br>
				</div>
				<div class="form-group">
					<label for="subtraction">Время выполнения вычитания (в миллисекундах):</label>
					<input type="text" class="form-control" id="subtraction" name="subtraction" value="` + subtraction + `"><br>
				</div>
				<div class="form-group">
					<label for="multiplication">Время выполнения умножения (в миллисекундах):</label>
					<input type="text" class="form-control" id="multiplication" name="multiplication" value="` + multiplication + `"><br>
				</div>
				<div class="form-group">
					<label for="division">Время выполнения деления (в миллисекундах):</label>
					<input type="text" class="form-control" id="division" name="division" value="` + division + `"><br>
				</div>
				<div class="form-group">
					<label for="exponent">Время выполнения степени (в миллисекундах):</label>
					<input type="text" class="form-control" id="exponent" name="exponent" value="` + exponent + `"><br>
				</div>
				<button type="submit" class="btn btn-primary">Вычислить</button>
			</form>
			<div id="result" class="mt-3"></div>
			<h2>Выполненные выражения:</h2>
			<ul class="list-group" id="expressionList">
	`
	if expressions != nil {
		for _, expr := range expressions {
			html += fmt.Sprintf("<li>%s = %d</li>", expr.Expression, expr.Result)
		}
	} else {
		html += `<h2>Вы не в системе или у вас ещё нет выражений!</h2>`
	}
	html += `
        	</ul>
    	</div>

    	<script src="https://code.jquery.com/jquery-3.5.1.slim.min.js"></script>
    	<script src="https://cdn.jsdelivr.net/npm/@popperjs/core@2.5.4/dist/umd/popper.min.js"></script>
    	<script src="https://maxcdn.bootstrapcdn.com/bootstrap/4.5.2/js/bootstrap.min.js"></script>

		<script>
		document.getElementById("expressionForm").addEventListener("submit", function(event) {
			event.preventDefault();
			var formData = new FormData(this);
			fetch("/calculate", {
				method: "POST",
				body: formData
			})
			.then(response => {
				// Клонируем ответ
				const clone = response.clone();
			
				// Пытаемся распарсить исходный ответ как JSON
				return response.json()
				.catch(() => {
					// Если не удается, возвращаем клонированный ответ как текст
					return clone.text();
				});
			})
			.then(data => {
				// Обрабатываем данные и выводим результат
				if (typeof data === 'object') {
					document.getElementById("result").innerHTML = '<div class="alert alert-success" role="alert">Результат: ' + data.result + '</div>';
				} else {
					document.getElementById("result").innerHTML = '<div class="alert alert-danger" role="alert">Ошибка: ' + data + '</div>';
				}
			})
			.catch(error => {
				console.error("Ошибка:", error);
				document.getElementById("result").innerHTML = '<div class="alert alert-danger" role="alert">Ошибка: ' + error + '</div>';
			});					
		});		
    </script>
	</body>
	</html>
	`
	fmt.Fprint(w, html)
}

func getCookieToken(r *http.Request) (string, error) {
	// Получаем куку с именем "token" из запроса
	cookie, err := r.Cookie("token")
	if err != nil {
		// Обработка ошибки, если кука не найдена или не может быть прочитана
		return "", errors.New("токен авторизации не найден")
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

	session, _ := store.Get(r, "session-name")

	// Получаем значения из POST-запроса
	session.Values["expression"] = expr
	session.Values["addition"] = r.FormValue("addition")
	session.Values["subtraction"] = r.FormValue("subtraction")
	session.Values["multiplication"] = r.FormValue("multiplication")
	session.Values["division"] = r.FormValue("division")
	session.Values["exponent"] = r.FormValue("exponent")
	session.Save(r, w)

	// Получаем куку с именем "token" из запроса
	cookie, err := r.Cookie("token")
	if err != nil {
		// Обработка ошибки, если кука не найдена или не может быть прочитана
		http.Error(w, "Токен авторизации не найден", http.StatusUnauthorized)
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

func updateAgentStatus(agentID string, status string) error {
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Ошибка открытия базы данных:", err)
	}

	defer db.Close()

	_, err = db.Exec("UPDATE agents SET status = ?, last_ping = ? WHERE id = ?", status, time.Now(), agentID)
	if err != nil {
		return err
	}
	return nil
}

// Обработчик для принятия пингов от агентов
func pingHandler(w http.ResponseWriter, r *http.Request) {
	// Обработка пинга от агента
	agentID := r.URL.Query().Get("id")
	if agentID == "" {
		http.Error(w, "Agent ID is required", http.StatusBadRequest)
		return
	}

	// Обновляем статус агента в базе данных
	if err := updateAgentStatus(agentID, "alive"); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error updating agent status: %v", err)
		return
	}

	log.Printf("Received ping from agent %s", agentID)

	// Получаем куку с именем "token" из запроса
	cookie, err := r.Cookie("token")
	if err != nil {
		// Обработка ошибки, если кука не найдена или не может быть прочитана
		http.Error(w, "Токен авторизации не найден", http.StatusUnauthorized)
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

	err = checkFreeExpressions(user)
	if err != nil {
		log.Fatal(err)
	}
	// Перенаправляем на страницу агентов
	http.Redirect(w, r, "/agents", http.StatusFound)
}

func checkFreeExpressions(user string) error {
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}

	// Проверяем, нет ли уже результата в базе данных
	rows, err := db.Query("SELECT expression, status FROM expressions")
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
	db.Close()
	rows.Close()

	_, err = HandleCalculateRequest(express, user, op1Int, op2Int, op3Int, op4Int, op5Int)
	if err != nil {
		log.Printf("Error while evaluate: %v", err)
	}

	return nil
}

func CheckAgentsAvailability(expr string) (bool, error) {
	// открываем базу данны
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Ошибка открытия базы данных:", err)
	}

	defer db.Close()

	var id int
	var status string

	// Выбираем информацию об агентах из базы данных
	rows, err := db.Query("SELECT id, status FROM agents")
	if err != nil {
		log.Printf("Error querying agents: %v", err)
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		if err := rows.Scan(&id, &status); err != nil {
			log.Printf("Error scanning agent row: %v", err)
			return false, err
		}
		if status == "alive" {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		log.Printf("Error iterating over agent rows: %v", err)
		return false, err
	}
	return false, nil
}

// Обработчик для отображения информации об агентах
func agentsHandler(w http.ResponseWriter, r *http.Request) {
	type AgentInfo struct {
		ID         string
		Status     string
		LastActive time.Time
	}

	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Ошибка открытия базы данных:", err)
	}
	defer db.Close()

	// Выбираем информацию об агентах из базы данных
	rows, err := db.Query("SELECT id, status, last_ping FROM agents")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error querying agents: %v", err)
		return
	}
	defer rows.Close()

	var agents []AgentInfo
	for rows.Next() {
		var agent AgentInfo
		if err := rows.Scan(&agent.ID, &agent.Status, &agent.LastActive); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			log.Printf("Error scanning agent row: %v", err)
			return
		}
		agents = append(agents, agent)
	}
	if err := rows.Err(); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error iterating over agent rows: %v", err)
		return
	}
	session, _ := store.Get(r, "session-name")

	// Получаем текущее значение таймаута из сессии
	timeoutStr := session.Values["timeout"]
	if timeoutStr == nil {
		timeoutStr = "10" // Значение по умолчанию
	}

	// Проверяем, было ли передано новое значение таймаута через параметр запроса
	if timeoutQuery := r.URL.Query().Get("timeout"); timeoutQuery != "" {
		timeoutStr = timeoutQuery
	}

	// Сохраняем новое значение таймаута в сессии
	session.Values["timeout"] = timeoutStr
	session.Save(r, w)

	// Переводим строку в интеджер
	timeout, err := strconv.Atoi(timeoutStr.(string))
	if err != nil {
		log.Printf("Error parsing timeout value: %v", err)
		http.Error(w, "Invalid timeout value", http.StatusBadRequest)
		return
	}

	// Проверяем агентов
	checkAgentStatus(timeout)

	// Отображаем страницу агентов с информацией из базы данных
	tmpl := template.Must(template.New("agents").Parse(`
			<!DOCTYPE html>
			<html>
			<head>
				<title>Агенты</title>
				<link rel="stylesheet" href="https://maxcdn.bootstrapcdn.com/bootstrap/4.5.2/css/bootstrap.min.css">
			</head>
			<body>
				<div class="container mt-5">
					<div class="jumbotron">
						<h1 class="display-4">Агенты</h1>
						<p class="lead">Страница для просмотра информации об агентах.</p>
						<hr class="my-4">
						<form action="/agents" method="get">
							<div class="form-group">
								<label for="timeout">Таймаут (в секундах):</label>
								<input type="number" class="form-control" id="timeout" name="timeout" value="` + timeoutStr.(string) + `">
							</div>
							<button type="submit" class="btn btn-primary">Обновить таймаут</button>
						</form>
						<table class="table">
							<thead>
								<tr>
									<th>ID</th>
									<th>Status</th>
									<th>Last Active</th>
								</tr>
							</thead>
							<tbody>
								{{range .}}
								<tr>
									<td>{{.ID}}</td>
									<td>{{.Status}}</td>
									<td>{{.LastActive}}</td>
								</tr>
								{{end}}
							</tbody>
						</table>
						<a class="btn btn-primary btn-lg" href="/" role="button">Назад к калькулятору</a>
					</div>
				</div>
			</body>
			</html>
	`))
	if err := tmpl.Execute(w, agents); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		log.Printf("Error executing template: %v", err)
	}
}

func getExpressions(user string) ([]Expression, error) {
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Ошибка открытия базы данных:", err)
	}

	defer db.Close()
	rows, err := db.Query("SELECT id, expression, result, status FROM expressions WHERE user = $1", user)
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
	result, err := computeExpression(expression, expressionID)
	if err != nil {
		return 0, err
	}

	// Обновляем результат вычисления в базе данных
	err = updateExpressionResult(expressionID, result, "success")
	if err != nil {
		return 0, err
	}

	//Cчитаем время
	total := Time(expression, op1, op2, op3, op4, op5)
	time.Sleep(time.Duration(total) * time.Millisecond)
	return result, nil
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

func getOneExpression(expr string, exprID int) (int, error) {
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
	defer db.Close()
	// Проверяем, нет ли уже результата в базе данных
	rows, err := db.Query("SELECT id, expression, result FROM expressions WHERE expression = ?", expr)
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

	return 0, nil
}

func saveExpression(expression, user string) (int, bool, bool, error) {
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
	defer db.Close()
	// Проверяем, нет ли уже результата в базе данных
	rows, err := db.Query("SELECT id, expression, result, status FROM expressions WHERE expression = ? AND user = ?", expression, user)
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
	res, err := db.Exec("INSERT INTO expressions (expression, status, user) VALUES (?, ?, ?)", expression, "pending", user)
	if err != nil {
		return 0, false, false, err
	}
	expressionID, err := res.LastInsertId()
	if err != nil {
		return 0, false, false, err
	}
	return int(expressionID), false, true, nil
}

func computeExpression(expression string, id int) (int, error) {
	// Формируем POST-запрос агенту для вычисления выражения
	requestData := map[string]string{"expression": expression}
	requestBody, err := json.Marshal(requestData)
	if err != nil {
		return 0, fmt.Errorf("ошибка формирования тела запроса: %v", err)
	}

	// Отправляем POST-запрос
	resp, err := http.Post("http://localhost:8081", "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		return 0, fmt.Errorf("ошибка отправки POST-запроса агенту: %v", err)
	}
	defer resp.Body.Close()

	err = updateExpressionResult(id, 0, "processing")
	if err != nil {
		return 0, fmt.Errorf("ошибка обновления базы данных: %v", err)
	}

	// Проверяем статус ответа
	fmt.Println("Response status:", resp.Status)
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("ошибка вычисления выражения: статус код %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("ошибка чтения тела ответа: %v", err)
	}

	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		panic(err)
	}
	str := obj["result"].(string)

	result, err := strconv.Atoi(str)
	if err != nil {
		return 0, fmt.Errorf("ошибка преобразования результата в число: %v", err)
	}

	return result, nil
}

func updateExpressionResult(expressionID int, result int, status string) error {
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
	defer db.Close()

	// Обновляем результат вычисления в базе данных
	_, err = db.Exec("UPDATE expressions SET result=?, status=? WHERE id=?", result, status, expressionID)
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

func checkAgentStatus(timeout int) {
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
	defer db.Close()

	// Получаем текущее время
	currentTime := time.Now()
	var agentID string
	var status string
	var lastPing time.Time
	var timeouttrue bool

	// Выбираем всех агентов из базы данных
	rows, err := db.Query("SELECT id, status, last_ping FROM agents")
	if err != nil {
		log.Printf("Error querying agents: %v", err)
		return
	}
	defer rows.Close()

	// Проверяем время последнего пинга каждого агента
	for rows.Next() {
		if err := rows.Scan(&agentID, &status, &lastPing); err != nil {
			log.Printf("Error scanning agent row: %v", err)
			continue
		}
		timeo := time.Duration(timeout) * time.Second

		// Если время последнего пинга превышает таймаут, помечаем агента как мертвого
		if currentTime.Sub(lastPing) > timeo && status != "dead" {
			timeouttrue = true
		}
	}
	if timeouttrue {
		_, err = db.Exec("UPDATE agents SET status = ?, last_ping = ? WHERE id = ?", "dead", time.Now(), agentID)
		if err != nil {
			log.Printf("Error updating agent status: %v", err)
		}
	}
}

func main() {
	// Подключаемся к базе данных
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
	defer db.Close()

	// Создаем таблицу для хранения выражений
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS expressions (id INTEGER PRIMARY KEY AUTOINCREMENT, expression TEXT, result INTEGER, status TEXT, user TEXT);")
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

	http.HandleFunc("/", orchestrateHandler)
	http.HandleFunc("/login", loginHandler)
	http.HandleFunc("/loginCheck", loginCheckHandler)
	http.HandleFunc("/register", registHandler)
	http.HandleFunc("/registerCheck", registerCheckHandler)
	http.HandleFunc("/calculate", calcHandler)
	http.HandleFunc("/agents", agentsHandler)
	http.HandleFunc("/ping", pingHandler)
	// http.HandleFunc("/agents", agentsHandler)

	// Запускаем сервер
	log.Fatal(http.ListenAndServe(":8080", nil))
}
