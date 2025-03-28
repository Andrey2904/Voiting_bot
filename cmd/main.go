package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	tarantool "github.com/tarantool/go-tarantool/v2"
)

var tnt *tarantool.Connection

type Poll struct {
	ID       string
	Question string
	Optinal  []string
	Votes    map[string]int
	Author   string
	Closed   bool
}

type Vote struct {
	PollID    string
	User      string
	OptionNum int
}

func LoadAllVotes(pollID string) ([]Vote, error) {
	req := tarantool.NewSelectRequest("poll_votes").
		Index("poll_id").
		Iterator(tarantool.IterEq).
		Key([]interface{}{pollID})

	resp, err := tnt.Do(req).Get()
	if err != nil {
		return nil, err
	}

	var result []Vote
	for _, tupleRaw := range resp {
		row := tupleRaw.([]interface{}) // [pollID, user, optionNum]
		v := Vote{
			PollID:    row[0].(string),
			User:      row[1].(string),
			OptionNum: int(row[2].(int64)), // msgpack распознаёт int как int64
		}
		result = append(result, v)
	}
	return result, nil
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dialer := tarantool.NetDialer{
		Address:  "tarantool-db:3301",
		User:     "admin",
		Password: "password",
	}
	opts := tarantool.Opts{
		Timeout: 1 * time.Second,
	}
	conn, err := tarantool.Connect(ctx, dialer, opts)
	if err != nil {
		log.Fatalf("Ошибка подключения к Tarantool: %v", err)
	}

	tnt = conn
	log.Println("✅ Подключение к Tarantool установлено")

	// теперь запускаем сервер
	http.HandleFunc("/command", handleCommand)

	log.Println("Бот слушает на :8080")
	err = http.ListenAndServe(":8080", nil)
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
	poll, err := LoadPoll(pollID)
	if err != nil {
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

	// Выполняем Lua update
	luaCode := `
		local id = ...
		box.space.polls:update(id, {{'=', 5, true}})
	`

	_, err = tnt.Eval(luaCode, []interface{}{pollID})

	if err != nil {
		respond(w, fmt.Sprintf("Ошибка при закрытии опроса: %v", err))
		return
	}
	respond(w, fmt.Sprintf("🔒 Опрос *%s* был закрыт. Голосование завершено.", poll.Question))
}

func handleResults(w http.ResponseWriter, pollID string, user string) {
	// 1. Загружаем опрос
	poll, err := LoadPoll(pollID)
	if err != nil {
		respond(w, "Опрос не найден: "+err.Error())
		return
	}

	// 2. Загружаем все голоса из poll_votes по pollID
	req := tarantool.NewSelectRequest("poll_votes").
		Index("poll_id").
		Iterator(tarantool.IterEq).
		Key([]interface{}{pollID})

	resp, err := tnt.Do(req).Get()
	if err != nil {
		respond(w, "Ошибка при загрузке голосов: "+err.Error())
		return
	}

	// 3. Подсчёт голосов
	counts := make([]int, len(poll.Optinal))
	for _, tuple := range resp {
		row := tuple.([]interface{})
		optionNum := int(row[2].(int64)) // поле option_num

		if optionNum >= 1 && optionNum <= len(poll.Optinal) {
			counts[optionNum-1]++
		}
	}

	// 4. Формируем сообщение с результатами
	msg := fmt.Sprintf("📊 Результаты опроса *%s*:\n", poll.Question)
	for i, option := range poll.Optinal {
		msg += fmt.Sprintf("%d. %s — %d голос(а)\n", i+1, option, counts[i])
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
	poll := &Poll{
		ID:       pollID,
		Question: question,
		Optinal:  options,
		Votes:    make(map[string]int),
		Author:   user,
		Closed:   false,
	}

	_, err := tnt.Insert("polls", []interface{}{
		poll.ID,
		poll.Question,
		poll.Optinal,
		poll.Author,
		poll.Closed,
	})

	if err != nil {
		log.Println("Ошибка сохранения опроса в Tarantool:", err)
	} else {
		log.Println("✅ Опрос сохранён в Tarantool:", poll.ID)
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
	poll, err := LoadPoll(pollID)

	if err != nil {
		respond(w, fmt.Sprintf("Опрос с ID `%s` не найден.", pollID))
		return
	}

	if poll.Closed {
		respond(w, "Голосование завершилось.")
		return
	}
	if optionNum > len(poll.Optinal) {
		respond(w, "Такого варианта не существует.")
		return
	}

	// 2) Проверим, голосовал ли user
	voted, err := CheckVote(pollID, user)
	if err != nil {
		respond(w, "Ошибка при проверке голоса.")
		return
	}
	if voted {
		respond(w, "Ты уже голосовал в этом опросе.")
		return
	}
	err = SaveVote(pollID, user, optionNum)
	if err != nil {
		respond(w, fmt.Sprintf("Ошибка сохранения голоса: %v", err))
		return
	}

	respond(w, fmt.Sprintf("✅ %s, ты проголосовал за *%s* в опросе *%s*.",
		user, poll.Optinal[optionNum-1], poll.Question))
}

func LoadPoll(pollID string) (*Poll, error) {
	req := tarantool.NewSelectRequest("polls").
		Index("primary").
		Limit(1).
		Iterator(tarantool.IterEq).
		Key([]interface{}{pollID})

	// Выполняем запрос
	resp, err := tnt.Do(req).Get()
	if err != nil {
		return nil, fmt.Errorf("ошибка при запросе к Tarantool: %v", err)
	}
	if len(resp) == 0 {
		return nil, fmt.Errorf("опрос с ID %s не найден", pollID)
	}
	t := resp[0].([]interface{})
	optionsRaw := t[2].([]interface{})
	options := make([]string, len(optionsRaw))
	for i, v := range optionsRaw {
		options[i] = v.(string)
	}
	poll := &Poll{
		ID:       t[0].(string),
		Question: t[1].(string),
		Optinal:  options,
		Author:   t[3].(string),
		Closed:   t[4].(bool),
		Votes:    make(map[string]int),
	}
	return poll, nil
}

func SaveVote(pollID, user string, optionNum int) error {
	req := tarantool.NewInsertRequest("poll_votes").
		Tuple([]interface{}{
			pollID,
			user,
			optionNum,
		})
	_, err := tnt.Do(req).Get()
	return err
}

func CheckVote(pollID, user string) (bool, error) {
	req := tarantool.NewSelectRequest("poll_votes").
		Index("pk"). // (poll_id, user)
		Limit(1).
		Iterator(tarantool.IterEq).
		Key([]interface{}{pollID, user})

	resp, err := tnt.Do(req).Get()
	if err != nil {
		return false, err
	}
	return len(resp) > 0, nil // если что-то нашли => уже голосовал
}
