# CalcFlow
Golang Project

Всё работает топорно конечно...

cначала скачаем sqlite и sessions если у вас его нет или go.mod барахлит:  
go get github.com/mattn/go-sqlite3  
go get github.com/gorilla/sessions

В общем, запускаем orchest.go по типу: 
go run backend/internal/orchestrator/orchest.go

дальше открываем новый терминал(вы всё ещё должны находится в проекте) и делаем тоже самое с агентом:  
go run backend/internal/agent/agent.go

Оркестратор:   
1.Есть html cтраница, в ней можно ввести выражение и время выполнения операций(+-*/^).   
2.После этого через POST-запрос оркестратору отправляется выражение с указанием времени выполнения операций.    
3.После этого через тот же самый пост))) он передаёт агенту на вычисления выражение и принимая результат выводит на страничку.   
4.При этом всё сохранятся в бд.   

p.s. я хотел реализовать ещё создание и выборку агентов, но не успел, но зарождение кода осталось...   
По крайне мере есть проверка состояния агента :)

Если описать агента, то вот:
По сути сервер на порту 8081. Принимает выражение ввиде JSON-а и вычисляет с помощью функции
в постфиксной нотации(доступны операции +-*/^). По идее там должно было FLOAT отдават, но у
меня что-то связанное с бд ошибка вылетает, так что при отправке оркестратору значение конвертируется
в интеджер, а затем и в строку(на стороне оркестратора результат превращается снова в интеджер через strconv.Atoi()).


Если есть вопросы, пишите в ТГ @NarzhanDev )
