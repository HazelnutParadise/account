package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"slices"
	"sync"

	"account/api"
	"account/lib"
	"account/obj"

	"account/db"

	"github.com/HazelnutParadise/Go-Utils/hashutil"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// 一天的秒數
const DAY = 24 * 3600

const supportDocsLink = "https://support.hazelnut-paradise.com/?category_id=2&category_name=%e6%9c%83%e5%93%a1%e8%88%87%e5%b8%b3%e8%99%9f"

// Session 名稱
const sessionName = "sessionid"

var DB = db.DB

var registerDataBuf = sync.Map{}
var emailVerifyCodeBuf = sync.Map{}

func main() {
	// gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 設置模板渲染引擎
	r.LoadHTMLGlob("templates/*")

	// 1. 建立一個 Cookie-based Store (可改用 RedisStore…)
	sessionKey := os.Getenv("SESSION_KEY")
	if sessionKey == "" {
		// 若沒有設定 SESSION_KEY，就用預設 (僅限開發)
		sessionKey = "secret123!@#"
	}
	store := cookie.NewStore([]byte(sessionKey))
	store.Options(sessions.Options{
		Path:     "/",
		HttpOnly: true,
		Secure:   true,     // 若正式站走 HTTPS，務必開啟
		MaxAge:   30 * DAY, // Session 有效期(秒)，可自行調整
		SameSite: http.SameSiteNoneMode,
	})

	// 2. 將 session middleware 註冊到 Gin
	r.Use(sessions.Sessions(sessionName, store))

	// 路由
	r.GET("/", homeHandler)
	r.GET("/register", registerPageHandler)
	r.POST("/register", registerHandler)
	r.GET("/verify-email", verifyEmailPageHandler)
	r.POST("/verify-email", verifyEmailHandler)
	r.GET("/login", loginPageHandler)
	r.POST("/login", loginHandler)
	r.GET("/profile", authRequiredPage(), profilePageHandler) // 需要登入
	r.POST("/profile", authRequiredPage(), profileEditHandler)
	r.GET("/logout", authRequiredPage(), logoutHandler)
	api.SetRoutes(r.Group("/api"), &registerDataBuf, &emailVerifyCodeBuf)

	// 啟動 Server
	err := r.Run(":3331") // 監聽在 3331 port
	if err != nil {
		panic(err)
	}
}

// -------------------------
// 首頁 (GET /)
// -------------------------
func homeHandler(c *gin.Context) {
	redirect := c.Query("redirect")
	if !lib.IsLoggedin(c) {
		if redirect != "" {
			c.Redirect(http.StatusFound, "/login?redirect="+redirect)
		} else {
			c.Redirect(http.StatusFound, "/login")
		}
		return
	}

	if redirect != "" {
		c.Redirect(http.StatusFound, redirect)
		return
	}

	c.Redirect(http.StatusFound, "/profile")
}

func registerPageHandler(c *gin.Context) {
	redirectUrl := c.Query("redirect")
	c.HTML(http.StatusOK, "register.html", struct {
		SupportDocsLink string
		RedirectUrl     string
	}{
		SupportDocsLink: supportDocsLink,
		RedirectUrl:     redirectUrl,
	})
}

// -------------------------
// 註冊 (POST /register)
// Body JSON: { "username": "...", "password": "..." }
// -------------------------
func registerHandler(c *gin.Context) {
	var req struct {
		Username        string `json:"username"`
		Password        string `json:"password"`
		PasswordConfirm string `json:"passwordConfirm"`
		Email           string `json:"email"`
		Phone           string `json:"phone"`
		Nickname        string `json:"nickname"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		lib.FastJSON(c, http.StatusBadRequest, lib.JsonError{Error: "無效的參數格式"})
		return
	}

	// 簡易檢查
	if slices.Contains([]string{req.Username, req.Password, req.Email, req.Phone, req.Nickname}, "") {
		lib.FastJSON(c, http.StatusBadRequest, lib.JsonError{Error: "請填寫所有欄位"})
		return
	}

	// 密碼確認
	if req.Password != req.PasswordConfirm {
		lib.FastJSON(c, http.StatusBadRequest, lib.JsonError{Error: "密碼確認不相符"})
		return
	}

	// 檢查是否已存在
	users := []obj.User{}
	DB.Find(&users, "username = ?", req.Username)
	fmt.Println(users)
	fmt.Println(len(users) == 0)
	if len(users) > 0 {
		lib.FastJSON(c, http.StatusConflict, lib.JsonError{Error: "該使用這名稱已存在，請換一個"})
		return
	}

	DB.Find(&users, "email = ?", req.Email)
	if len(users) > 0 {
		lib.FastJSON(c, http.StatusConflict, lib.JsonError{Error: "這個 Email 已被註冊，請登入現有帳號"})
		return
	}

	DB.Find(&users, "phone = ?", req.Phone)
	if len(users) > 0 {
		lib.FastJSON(c, http.StatusConflict, lib.JsonError{Error: "這個手機號碼已被註冊，請登入現有帳號"})
		return
	}

	// 雜湊密碼
	hashedPassword, hashSalt, err := hashutil.Hash(req.Password)
	if err != nil {
		lib.FastJSON(c, http.StatusInternalServerError, lib.JsonError{Error: "無法雜湊密碼"})
		return
	}

	newUser := obj.User{
		Username: req.Username,
		Password: hashedPassword,
		Salt:     hashSalt,
		Email:    req.Email,
		Phone:    req.Phone,
		Nickname: req.Nickname,
	}

	tempUserUUID := uuid.New().String()
	registerDataBuf.Store(tempUserUUID, newUser)

	lib.FastJSON(c, 200, struct {
		Message  string `json:"message"`
		DataUUID string `json:"dataUUID"`
	}{
		Message:  "接下來請驗證 Email",
		DataUUID: tempUserUUID,
	})
}

func loginPageHandler(c *gin.Context) {
	c.HTML(http.StatusOK, "login.html", struct {
		SupportDocsLink string
		RedirectUrl     string
	}{
		SupportDocsLink: supportDocsLink,
		RedirectUrl:     c.Query("redirect"),
	})
}

// -------------------------
// 登入 (POST /login)
// Body JSON: { "username": "...", "password": "..." }
// -------------------------
func loginHandler(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		lib.FastJSON(c, http.StatusBadRequest, lib.JsonError{Error: "無效的參數格式"})
		fmt.Println("JSON 綁定錯誤:", err)
		return
	}

	users := []obj.User{}
	var user obj.User
	DB.Find(&users, "username = ?", req.Username)
	if len(users) == 0 {
		lib.FastJSON(c, http.StatusUnauthorized, lib.JsonError{Error: "使用者不存在"})
		fmt.Println("使用者不存在:", req.Username)
		return
	}

	user = users[0]

	// 檢查密碼
	if !hashutil.CompareHash(req.Password, user.Password, user.Salt) {
		lib.FastJSON(c, http.StatusUnauthorized, lib.JsonError{Error: "密碼錯誤"})
		fmt.Println("密碼錯誤:", req.Username)
		return
	}

	// 生成新的 session ID
	sessionID := lib.CreateSessionInDB(user.ID)

	// 成功後寫入 Session
	session := sessions.Default(c)
	session.Set("sessionID", sessionID)
	if err := session.Save(); err != nil {
		lib.FastJSON(c, http.StatusInternalServerError, lib.JsonError{Error: "無法儲存 session"})
		fmt.Println("無法儲存 session:", err)
		return
	}

	// 驗證 session 是否成功寫入
	savedSessionID := session.Get("sessionID")
	if savedSessionID == nil {
		lib.FastJSON(c, http.StatusInternalServerError, lib.JsonError{Error: "session 寫入失敗"})
		fmt.Println("session 寫入失敗")
		return
	}

	lib.FastJSON(c, http.StatusOK, lib.JsonMessage{Message: "登入成功"})
	fmt.Println("登入成功, sessionID:", savedSessionID)
}

// -------------------------
// 受保護路由 (GET /profile)
// 需登入才能查看: 使用 "authRequired" middleware
// -------------------------
func profilePageHandler(c *gin.Context) {
	session := sessions.Default(c)
	sessionID := session.Get("sessionID").(string)
	var sessionInDB obj.Session
	DB.First(&sessionInDB, "id = ?", sessionID)

	userID := sessionInDB.UserID

	// 顯示個人資訊 (示範: 只顯示 username)
	var user obj.User
	DB.First(&user, "id = ?", userID)
	c.HTML(http.StatusOK, "profile.html", struct {
		User obj.User
	}{
		User: obj.User{
			Username: user.Username,
			Phone:    user.Phone,
			Nickname: user.Nickname,
			Email:    user.Email,
		},
	})
}

func profileEditHandler(c *gin.Context) {
	session := sessions.Default(c)
	sessionID := session.Get("sessionID").(string)
	var sessionInDB obj.Session
	DB.First(&sessionInDB, "id = ?", sessionID)
	userID := sessionInDB.UserID

	// 顯示個人資訊 (示範: 只顯示 username)
	var user obj.User
	DB.First(&user, "id = ?", userID)

	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Phone    string `json:"phone"`
		Nickname string `json:"nickname"`
		Bio      string `json:"bio"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		lib.FastJSON(c, http.StatusBadRequest, lib.JsonError{Error: "無效的參數格式"})
		fmt.Println("JSON 綁定錯誤:", err)
		return
	}

	// 更新使用者資訊
	DB.Model(&user).Updates(struct {
		email    string
		phone    string
		nickname string
	}{
		email:    req.Email,
		phone:    req.Phone,
		nickname: req.Nickname,
	})

	lib.FastJSON(c, http.StatusOK, lib.JsonMessage{Message: "更新成功"})
}

// -------------------------
// 登出 (GET /logout)
// -------------------------
func logoutHandler(c *gin.Context) {
	session := sessions.Default(c)
	sessionID := session.Get("sessionID").(string)

	// 刪除 session 資料
	lib.DisableSessionInDB(sessionID)

	// 設定 session 過期
	session.Options(sessions.Options{
		MaxAge: -1,
	})
	// 清除 session
	session.Clear()
	session.Save()

	lib.FastJSON(c, http.StatusOK, lib.JsonMessage{Message: "登出成功"})
}

func verifyEmailPageHandler(c *gin.Context) {
	dataUUID := c.Query("data_uuid")
	if dataUUID == "" {
		lib.FastJSON(c, 400, lib.JsonError{Error: "資料載入失敗"})
		return
	}
	data, ok := registerDataBuf.Load(dataUUID)
	if !ok {
		lib.FastJSON(c, 400, lib.JsonError{Error: "資料載入失敗"})
		return
	}
	registerData := data.(obj.User)
	code := lib.GenerateVerifyCode(7)
	err := lib.SendEmailVerifyCode(registerData.Email, code)
	if err != nil {
		lib.FastJSON(c, 500, lib.JsonError{Error: "發送Email失敗"})
		return
	}
	emailVerifyCodeBuf.Store(dataUUID, code)
	c.HTML(http.StatusOK, "verify-email.html", struct {
		Email      string
		DataUUID   string
		BackendURI string
	}{
		Email:      registerData.Email,
		DataUUID:   dataUUID,
		BackendURI: "/verify-email",
	})
}

func verifyEmailHandler(c *gin.Context) {
	var verifyData = struct {
		Type      string `json:"type"`
		DataUUID  string `json:"dataUUID"`
		InputCode string `json:"inputCode"`
	}{}
	err := json.NewDecoder(c.Request.Body).Decode(&verifyData)
	if err != nil {
		lib.FastJSON(c, 400, struct {
			Error string `json:"error"`
		}{
			Error: "Invalid request body",
		})
		return
	}

	switch verifyData.Type {
	case "email":
		code, ok := emailVerifyCodeBuf.Load(verifyData.DataUUID)
		if !ok {
			lib.FastJSON(c, 404, lib.JsonError{Error: "找不到對應的資料"})
			return
		}
		if code != verifyData.InputCode {
			lib.FastJSON(c, 400, lib.JsonError{Error: "輸入的驗證碼不正確"})
			return
		}
		emailVerifyCodeBuf.Delete(verifyData.DataUUID)

		// 如果是註冊，存進資料庫
		if userData, ok := registerDataBuf.Load(verifyData.DataUUID); ok {
			log.Println(userData)
			user := userData.(obj.User)
			dbRes := db.DB.Save(&user)
			registerDataBuf.Delete(verifyData.DataUUID)
			if dbRes.Error != nil {
				lib.FastJSON(c, 500, lib.JsonError{Error: "註冊失敗"})
				return
			}

			lib.FastJSON(c, 200, lib.JsonMessage{Message: "註冊成功"})
			return
		}

	}

}

// -------------------------
// Middleware: authRequired
// 用於需要登入的路由
// -------------------------
func authRequiredPage() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !lib.IsLoggedin(c) {
			c.Redirect(http.StatusFound, "/login")
			c.Abort()
			return
		}
		c.Next()
	}
}

// func authRequiredApi() gin.HandlerFunc {
// 	return func(c *gin.Context) {
// 		if !lib.IsLoggedin(c) {
// 			lib.FastJSON(c, http.StatusUnauthorized, lib.JsonError{Error: "未登入"})
// 			c.Abort()
// 			return
// 		}
// 		c.Next()
// 	}
// }
