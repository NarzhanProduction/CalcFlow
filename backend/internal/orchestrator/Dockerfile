FROM golang:1.22.1

WORKDIR /build

COPY . ./

WORKDIR ./backend/internal/orchestrator

# Копируем файлы фронтенда внутрь контейнера
COPY frontend/agentsAndmain ./frontend/agentsAndmain
COPY frontend/regAndLog ./frontend/regAndLog

RUN go build -o orchestrator orchest.go


CMD ["./orchestrator"]