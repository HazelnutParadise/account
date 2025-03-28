package lib

import (
	"bytes"
	"net/http"
)

func SendEmailVerifyCode(email string, code string) error {
	mailStruct := struct {
		Recipient string `json:"recipient"`
		Type      string `json:"type"`
		Content   string `json:"content"`
	}{
		Recipient: email,
		Type:      "verify",
		Content:   code,
	}
	milJson, err := json.Marshal(mailStruct)
	if err != nil {
		return err
	}
	res, err := http.Post("http://sendemail/SendEmail", "application/json", bytes.NewBuffer(milJson))
	if err != nil {
		res, err = http.Post("http://192.168.1.164:6661/SendEmail", "application/json", bytes.NewBuffer(milJson))
		if err != nil {
			return err
		}
	}
	defer res.Body.Close()
	return nil
}
