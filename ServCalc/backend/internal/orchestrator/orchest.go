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
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Result struct {
	Result string `json:"result"`
}

type Expression struct {
	ID         int
	Expression string
	Result     int
	Status     string
}

func orchestrateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	expressions, err := getExpressions()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
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
            	<h1 class="display-4">Арифметический калькулятор</h1>
            	<hr class="my-4">
        	</div>
			<h1>Арифметический калькулятор</h1>
			<form id="expressionForm">
				<div class="form-group">
					<label for="expression">Введите выражение:</label>
					<input type="text" class="form-control" id="expression" name="expression">
				</div>
				<div class="form-group">
					<label for="addition">Время выполнения сложения (в миллисекундах):</label>
					<input type="text" class="form-control" id="addition" name="addition" value="200">
				</div>
				<div class="form-group">
					<label for="subtraction">Время выполнения вычитания (в миллисекундах):</label>
					<input type="text" class="form-control" id="subtraction" name="subtraction" value="200">
				</div>
				<div class="form-group">
					<label for="multiplication">Время выполнения умножения (в миллисекундах):</label>
					<input type="text" class="form-control" id="multiplication" name="multiplication" value="200">
				</div>
				<div class="form-group">
					<label for="division">Время выполнения деления (в миллисекундах):</label>
					<input type="text" class="form-control" id="division" name="division" value="200">
				</div>
				<div class="form-group">
					<label for="exponent">Время выполнения степени (в миллисекундах):</label>
					<input type="text" class="form-control" id="exponent" name="exponent" value="200">
				</div>
				<button type="submit" class="btn btn-primary">Вычислить</button>
			</form>
			<div id="result" class="mt-3"></div>
			<h2>Выполненные выражения:</h2>
			<ul class="list-group" id="expressionList">
	`
	for _, expr := range expressions {
		html += fmt.Sprintf("<li>%s = %d</li>", expr.Expression, expr.Result)
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
            .then(response => response.json())
            .then(data => {
                if (data.error) {
                    document.getElementById("result").innerText = data.error;
                } else {
                    document.getElementById("result").innerText = "Результат: " + data.result;
                    var li = document.createElement("li");
                    li.appendChild(document.createTextNode(formData.get("expression") + " = " + data.result));
                    document.getElementById("expressionList").appendChild(li);
                }
            })
            .catch(error => {
                console.error("Ошибка:", error);
            });
        });
    </script>
	</body>
	</html>
	`
	fmt.Fprintf(w, html)
}

// Обработчик для вычисления выражения
func calcHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	expr := r.FormValue("expression")
	if expr == "" {
		http.Error(w, "Expression is required", http.StatusBadRequest)
		return
	}
	add, err := strconv.Atoi(r.FormValue("addition"))
	if err != nil {
		http.Error(w, "Invalid addition speed", http.StatusBadRequest)
		return
	}
	subt, err := strconv.Atoi(r.FormValue("subtraction"))
	if err != nil {
		http.Error(w, "Invalid subtraction speed", http.StatusBadRequest)
		return
	}
	multip, err := strconv.Atoi(r.FormValue("multiplication"))
	if err != nil {
		http.Error(w, "Invalid multiplication speed", http.StatusBadRequest)
		return
	}
	div, err := strconv.Atoi(r.FormValue("division"))
	if err != nil {
		http.Error(w, "Invalid division speed", http.StatusBadRequest)
		return
	}
	exp, err := strconv.Atoi(r.FormValue("exponent"))
	if err != nil {
		http.Error(w, "Invalid division speed", http.StatusBadRequest)
		return
	}

	result, err := HandleCalculateRequest(expr, add, subt, multip, div, exp)
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
}

func agentsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	html := `
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
				<a class="btn btn-primary btn-lg" href="/" role="button">Назад к калькулятору</a>
			</div>
			<!-- Здесь можно добавить информацию о доступных агентах -->
		</div>
	</body>
	</html>
	`

	fmt.Fprintf(w, html)
}

func getExpressions() ([]Expression, error) {
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Ошибка открытия базы данных:", err)
	}

	defer db.Close()
	rows, err := db.Query("SELECT id, expression, result, status FROM expressions")
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

func HandleCalculateRequest(expression string, op1, op2, op3, op4, op5 int) (int, error) {
	// Проверяем выражение
	if !isValidExpression(expression) {
		return 0, errors.New("invalid expression")
	}

	// Записываем выражение в базу данных
	expressionID, isInSQL, err := saveExpression(expression)
	if err != nil {
		return 0, err
	}
	if isInSQL {
		resultReady, err := getOneExpression(expression, expressionID)
		if err != nil {
			return 0, err
		}

		return resultReady, nil
	}

	// Отправляем выражение агенту для вычисления
	result, err := computeExpression(expression)
	if err != nil {
		return 0, err
	}

	// Обновляем результат вычисления в базе данных
	err = updateExpressionResult(expressionID, result)
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

func saveExpression(expression string) (int, bool, error) {
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
	defer db.Close()
	// Проверяем, нет ли уже результата в базе данных
	rows, err := db.Query("SELECT id, expression, result, status FROM expressions WHERE expression = ?", expression)
	if err != nil {
		return 0, false, fmt.Errorf("database error: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var express, status string
		var result sql.NullInt64

		if err := rows.Scan(&id, &express, &result, &status); err != nil {
			return 0, false, fmt.Errorf("error scanning row: %v ", err)
		}

		if status == "success" && express == expression {
			return id, true, nil
		}
	}

	// Пишем выражение в базу данных и возвращаем его ID
	res, err := db.Exec("INSERT INTO expressions (expression, status) VALUES (?, ?)", expression, "pending")
	if err != nil {
		return 0, false, err
	}
	expressionID, err := res.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	return int(expressionID), false, nil
}

func checkAgentAvailability() {
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
	defer db.Close()
	// Получаем список всех агентов из базы данных
	rows, err := db.Query("SELECT id, hostname, port, last_ping FROM agents")
	if err != nil {
		log.Println("Error querying agents:", err)
		return
	}
	defer rows.Close()

	// Итерируемся по агентам и проверяем их доступность
	for rows.Next() {
		var id int
		var hostname, port string
		var lastPing time.Time
		if err := rows.Scan(&id, &hostname, &port, &lastPing); err != nil {
			log.Println("Error scanning agent row:", err)
			continue
		}

		// Проверяем время последнего пинга
		if time.Since(lastPing) > 2*time.Minute {
			// Агент недоступен, обновляем его статус в базе данных
			if err := updateAgentStatus(id, false); err != nil {
				log.Println("Error updating agent status:", err)
			}
		}
	}

	// Проверяем ошибки после итерации по результатам
	if err := rows.Err(); err != nil {
		log.Println("Error iterating over agent rows:", err)
	}
}

func updateAgentStatus(agentID int, isAvailable bool) error {
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
	defer db.Close()
	// Обновляем статус агента в базе данных
	_, err = db.Exec("UPDATE agents SET is_available=?, last_ping=? WHERE id=?", isAvailable, time.Now(), agentID)
	return err
}

func computeExpression(expression string) (int, error) {
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

func updateExpressionResult(expressionID int, result int) error {
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
	defer db.Close()
	// Обновляем результат вычисления в базе данных
	_, err = db.Exec("UPDATE expressions SET result=?, status=? WHERE id=?", result, "success", expressionID)
	return err
}

func isValidExpression(expression string) bool {
	regex := `^[\d\+\-\*\/\(\)\^]+$`

	// Проверяем выражение с помощью регулярного выражения
	match, err := regexp.MatchString(regex, expression)
	if err != nil {
		log.Println("Error checking expression validity:", err)
		return false
	}

	return match
}

func main() {
	// Подключаемся к базе данных
	db, err := sql.Open("sqlite3", "./backend/pkg/sql/expressions.db")
	if err != nil {
		log.Fatal("Error opening database:", err)
	}
	defer db.Close()

	// Создаем таблицу для хранения выражений
	_, err = db.Exec("CREATE TABLE IF NOT EXISTS expressions (id INTEGER PRIMARY KEY AUTOINCREMENT, expression TEXT, result INTEGER, status TEXT)")
	if err != nil {
		log.Fatal("Error creating table:", err)
	}

	_, err = db.Exec("CREATE TABLE IF NOT EXISTS agents (id INTEGER PRIMARY KEY AUTOINCREMENT, hostname TEXT NOT NULL, port TEXT NOT NULL, last_ping TIMESTAMP DEFAULT CURRENT_TIMESTAMP);")
	if err != nil {
		log.Fatal("Error creating table:", err)
	}

	http.HandleFunc("/", orchestrateHandler)
	http.HandleFunc("/calculate", calcHandler)
	http.HandleFunc("/agents", agentsHandler)
	// http.HandleFunc("/agents", agentsHandler)

	// Запускаем сервер
	log.Fatal(http.ListenAndServe(":8080", nil))

	//Время выполнения операций(да, надо изменять код). Выполняется в Миллисекундах.
	// opPlus := 1000
	// opMinus := 2000
	// opDivide := 2000
	// opmult := 100
	// opexp := 2000

	// // Бесконечный цикл ввода выражений
	// scanner := bufio.NewScanner(os.Stdin)
	// for {
	// 	fmt.Print("Введите выражение: ")
	// 	scanner.Scan()
	// 	expression := scanner.Text()

	// 	result, err := HandleCalculateRequest(expression, opPlus, opMinus, opDivide, opmult, opexp)
	// 	if err != nil {
	// 		fmt.Println("Ошибка:", err)
	// 	} else {
	// 		fmt.Println("Результат:", result)
	// 	}
	// }
}
