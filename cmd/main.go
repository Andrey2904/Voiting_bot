package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Poll struct {
	ID       string
	Question string
	Optinal  []string
	Votes    map[string]int
	Author   string
	Closed   bool
}

var polls = make(map[string]*Poll)

func main() {
	http.HandleFunc("/command", handleCommand)

	log.Println("Бот слушает на :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		log.Fatalf("Ошибка запуска сервера: %v", err)
	}
}

func handleCommand(w http.ResponseWriter, r *http.Request) {

	r.ParseForm()
	text := r.FormValue("text")
	user := r.FormValue("user_name")

	log.Printf("Команда от %s: %s", user, text)

	if strings.HasPrefix(text, "create ") {
		handleCreatePoll(w, text[7:], user)
		return
	}
	if strings.HasPrefix(text, "vote ") {
		handleVote(w, text[5:], user)
		return
	}

	if strings.HasPrefix(text, "results ") {
		handleResults(w, text[8:], user)
		return
	}
	if strings.HasPrefix(text, "close ") {
		handleClose(w, text[6:], user)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"text": "Неизвестная команда. Используй: `/poll create Вопрос | Вариант1 | Вариант2`",
	})
}

func handleClose(w http.ResponseWriter, pollID, user string) {
	poll, ok := polls[pollID]
	if !ok {
		respond(w, fmt.Sprintf("Опрос с ID `%s` не найден.", pollID))
		return
	}

	if poll.Closed {
		respond(w, "Этот опрос уже был закрыт.")
		return
	}

	if poll.Author != user {
		respond(w, "Ты не можешь закрыть этот опрос, так как ты его не создавал.")
		return
	}

	poll.Closed = true
	respond(w, fmt.Sprintf("🔒 Опрос *%s* был закрыт. Голосование завершено.", poll.Question))
}

func handleResults(w http.ResponseWriter, pollID string, user string) {
	poll, ok := polls[pollID]
	if !ok {
		respond(w, fmt.Sprintf("Опрос с ID `%s` не найден.", pollID))
		return
	}
	counts := make([]int, len(poll.Optinal))

	for _, vote := range poll.Votes {
		if vote > 0 && vote <= len(poll.Optinal) {
			counts[vote-1] += 1
		}
	}

	msg := fmt.Sprintf("📊 Результаты опроса *%s*:\n", poll.Question)
	for i, opt := range poll.Optinal {
		msg += fmt.Sprintf("%d. %s — %d голос(а)\n", i+1, opt, counts[i])
	}

	respond(w, msg)

}

func handleCreatePoll(w http.ResponseWriter, text, user string) {
	parts := strings.Split(text, "|")
	if len(parts) < 2 {
		respond(w, "Неправильный формат. Пример:\n`/poll create Какой язык? | Go | Rust | Python`")
		return
	}
	question := strings.TrimSpace(parts[0])
	var options []string
	for _, opt := range parts[1:] {
		options = append(options, strings.TrimSpace(opt))
	}
	pollID := fmt.Sprintf("poll-%d", time.Now().UnixNano())
	polls[pollID] = &Poll{
		ID:       pollID,
		Question: question,
		Optinal:  options,
		Votes:    make(map[string]int),
		Author:   user,
	}
	msg := fmt.Sprintf("Создан опрос *%s* (ID: `%s`):\n", question, pollID)
	for i, opt := range options {
		msg += fmt.Sprintf("%d. %s\n", i+1, opt)
	}
	msg += fmt.Sprintf("\nЧтобы проголосовать: `/poll vote %s номер_варианта`", pollID)

	respond(w, msg)
}

func respond(w http.ResponseWriter, text string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"response_type": "in_channel",
		"text":          text,
	})
}

func handleVote(w http.ResponseWriter, text, user string) {

	parts := strings.Fields(text)
	if len(parts) != 2 {
		respond(w, "Неправильный формат. Пример: `/poll vote poll-123456789 2`")
		return
	}
	pollID := parts[0]

	optionNum, err := strconv.Atoi(parts[1])

	if err != nil || optionNum < 1 {
		respond(w, "Номер варианта должен быть положительным числом.")
		return
	}
	poll, ok := polls[pollID]

	if !ok {
		respond(w, fmt.Sprintf("Опрос с ID `%s` не найден.", pollID))
		return
	}
	if poll.Closed {
		respond(w, "Голосование завершилось.")
		return
	}

	if _, voted := poll.Votes[user]; voted {
		respond(w, "Ты уже голосовал в этом опросе.")
		return
	}
	if optionNum > len(poll.Optinal) {
		respond(w, "Такого варианта не существует.")
		return
	}

	poll.Votes[user] = optionNum

	respond(w, fmt.Sprintf("✅ %s, ты проголосовал за *%s* в опросе *%s*.",
		user, poll.Optinal[optionNum-1], poll.Question))
}
