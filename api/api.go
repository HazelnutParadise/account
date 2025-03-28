package api

import (
	"account/db"
	"account/lib"
	"account/obj"
	"sync"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

func SetRoutes(r *gin.RouterGroup, registerDataBuf *sync.Map, emailVerifyCodeBuf *sync.Map) {
	r.GET("/user-info", getUserInfo)
}

func getUserInfo(c *gin.Context) {
	var isLoggedIn bool
	var user = obj.User{}
	session := sessions.Default(c)
	sessionID := session.Get("sessionID").(string)
	if sessionID != "" && lib.IsSessionActive(sessionID) {
		isLoggedIn = true

		var sessionInDB obj.Session
		db.DB.First(&sessionInDB, "id = ?", sessionID)

		userID := sessionInDB.UserID
		db.DB.First(&user, "id = ?", userID)
	}
	type userInfo struct {
		Username string `json:"username"`
		Phone    string `json:"phone"`
		Nickname string `json:"nickname"`
		Email    string `json:"email"`
	}
	lib.FastJSON(c, 200, struct {
		IsLoggedIn bool     `json:"isLoggedIn"`
		Info       userInfo `json:"info"`
	}{
		IsLoggedIn: isLoggedIn,
		Info: userInfo{
			Username: user.Username,
			Phone:    user.Phone,
			Nickname: user.Nickname,
			Email:    user.Email,
		},
	})
}
