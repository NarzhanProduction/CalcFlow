package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Result struct {
	Result string `json:"result"`
}

type Orchestrator struct {
	Database *sql.DB // База данных для хранения результатов
}

func NewOrchestrator(db *sql.DB) *Orchestrator {
	return &Orchestrator{
		Database: db,
	}
}

func (o *Orchestrator) HandleCalculateRequest(expression string) (int, error) {
	// Проверяем выражение
	if !isValidExpression(expression) {
		return 0, errors.New("invalid expression")
	}

	// Записываем выражение в базу данных
	expressionID, err := o.saveExpression(expression)
	if err != nil {
		return 0, err
	}

	// Отправляем выражение агенту для вычисления
	result, err := o.computeExpression(expression)
	if err != nil {
		return 0, err
	}

	// Обновляем результат вычисления в базе данных
	err = o.updateExpressionResult(expressionID, result)
	if err != nil {
		return 0, err
	}

	return result, nil
}

func (o *Orchestrator) saveExpression(expression string) (int, error) {
	// Проверяем, нет ли уже результата в базе данных
	rows, err := o.Database.Query("SELECT id, expression, result, status FROM expressions WHERE expression = ?", expression)
	if err != nil {
		return 0, fmt.Errorf("database error: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var express, status string
		var result sql.NullInt64

		if err := rows.Scan(&id, &express, &result, &status); err != nil {
			return 0, fmt.Errorf("error scanning row: %v ", err)
		}

		// Проверяем, было ли значение result сканировано успешно
		var resultValue int
		if result.Valid {
			resultValue = int(result.Int64)
		} else {
			resultValue = 0 // Или любое другое значение по умолчанию
		}

		if status == "success" && express == expression {
			return resultValue, nil
		}
	}

	// Пишем выражение в базу данных и возвращаем его ID
	res, err := o.Database.Exec("INSERT INTO expressions (expression, status) VALUES (?, ?)", expression, "pending")
	if err != nil {
		return 0, err
	}
	expressionID, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return int(expressionID), nil
}

func (o *Orchestrator) checkAgentAvailability() {
	// Получаем список всех агентов из базы данных
	rows, err := o.Database.Query("SELECT id, hostname, port, last_ping FROM agents")
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
			if err := o.updateAgentStatus(id, false); err != nil {
				log.Println("Error updating agent status:", err)
			}
		}
	}

	// Проверяем ошибки после итерации по результатам
	if err := rows.Err(); err != nil {
		log.Println("Error iterating over agent rows:", err)
	}
}

func (o *Orchestrator) updateAgentStatus(agentID int, isAvailable bool) error {
	// Обновляем статус агента в базе данных
	_, err := o.Database.Exec("UPDATE agents SET is_available=?, last_ping=? WHERE id=?", isAvailable, time.Now(), agentID)
	return err
}

func (o *Orchestrator) computeExpression(expression string) (int, error) {

	// Отправляем GET-запрос агенту для вычисления выражения
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

func (o *Orchestrator) updateExpressionResult(expressionID int, result int) error {
	// Обновляем результат вычисления в базе данных
	_, err := o.Database.Exec("UPDATE expressions SET result=?, status=? WHERE id=?", result, "success", expressionID)
	return err
}

func isValidExpression(expression string) bool {
	regex := `^[\d\+\-\*\/\(\)]+$`

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

	orchestrator := NewOrchestrator(db)

	// Бесконечный цикл ввода выражений
	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("Введите выражение: ")
		scanner.Scan()
		expression := scanner.Text()

		result, err := orchestrator.HandleCalculateRequest(expression)
		if err != nil {
			fmt.Println("Ошибка:", err)
		} else {
			fmt.Println("Результат:", result)
		}
	}
}
