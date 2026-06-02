package services

import (
	"auth-system/internal/utils"
	"encoding/json"
	"errors"
	"fmt"

	"os"
	"strings"
	"sync"
	"time"

	"auth-system/internal/config"

	"github.com/wneessen/go-mail"
)

var (
	ErrEmailNilConfig          = errors.New("email config is nil")
	ErrEmailTemplateNotFound   = errors.New("email template not found")
	ErrEmailTextsNotFound      = errors.New("email texts not found")
	ErrEmailInvalidTexts       = errors.New("invalid email texts format")
	ErrEmailEmptyRecipient     = errors.New("email recipient is empty")
	ErrEmailEmptySubject       = errors.New("email subject is empty")
	ErrEmailSMTPConfigMissing  = errors.New("SMTP configuration is missing")
	ErrEmailClientCreateFailed = errors.New("failed to create SMTP client")
	ErrEmailSendFailed         = errors.New("failed to send email")
)

const (
	defaultLanguage   = "zh-CN"
	defaultEmailType  = "register"
	smtpTimeout       = 15 * time.Second
	smtpPort465       = 465
	smtpPort587       = 587
	templatePath      = "dist/data/email-template.html"
	textsPath         = "dist/data/email-texts.json"
	connMaxIdleTime   = 5 * time.Minute
	connCheckInterval = 30 * time.Second
)

// templatePlaceholders 模板占位符列表
var templatePlaceholders = []string{
	"{{PAGE_TITLE}}",
	"{{GREETING}}",
	"{{DESCRIPTION}}",
	"{{VERIFY_URL}}",
	"{{BUTTON_TEXT}}",
	"{{LINK_HINT}}",
	"{{EXPIRE_NOTICE}}",
	"{{SECURITY_TIP}}",
	"{{FOOTER}}",
}

// EmailTexts 邮件文案
// 结构：language -> section -> key -> value
type EmailTexts map[string]map[string]map[string]string

// EmailService 邮件服务
type EmailService struct {
	cfg      *config.Config
	template string
	texts    EmailTexts
	mu       sync.RWMutex

	// 连接池相关
	client     *mail.Client
	clientMu   sync.Mutex
	lastUsed   time.Time
	stopKeeper chan struct{}

	// 异步发送追踪
	wg sync.WaitGroup
}

// NewEmailService 创建邮件服务
func NewEmailService(cfg *config.Config) (*EmailService, error) {
	if cfg == nil {
		return nil, ErrEmailNilConfig
	}

	if err := validateSMTPConfig(cfg); err != nil {
		return nil, err
	}

	template, err := loadTemplate(templatePath)
	if err != nil {
		return nil, err
	}

	texts, err := loadTexts(textsPath)
	if err != nil {
		return nil, err
	}

	if err := validateTemplateAndTexts(template, texts); err != nil {
		utils.LogWarn("EMAIL", "Template validation warning", fmt.Sprintf("error=%v", err))
		// 不返回错误，只记录警告
	}

	utils.LogInfo("EMAIL", fmt.Sprintf("Email service initialized: host=%s, port=%d, from=%s",
		cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom))

	service := &EmailService{
		cfg:        cfg,
		template:   template,
		texts:      texts,
		stopKeeper: make(chan struct{}),
	}

	go service.connectionKeeper()

	return service, nil
}

// VerifyConnection 验证 SMTP 连接
func (s *EmailService) VerifyConnection() error {
	if s == nil {
		return errors.New("email service is nil")
	}

	client, err := s.createClient()
	if err != nil {
		utils.LogError("EMAIL", "VerifyConnection", err, "SMTP connection verification failed")
		return fmt.Errorf("SMTP connection failed: %w", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			utils.LogWarn("EMAIL", "Failed to close SMTP client", "")
		}
	}()

	utils.LogInfo("EMAIL", "SMTP connection verified successfully")
	return nil
}

// SendVerificationEmailAsync 异步发送验证邮件（不阻塞调用方）
func (s *EmailService) SendVerificationEmailAsync(to, emailType, language, verifyURL, logContext string) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				utils.LogError(logContext, "SendVerificationEmailAsync",
					fmt.Errorf("panic: %v", r), fmt.Sprintf("to=%s, type=%s", to, emailType))
			}
		}()
		if err := s.SendVerificationEmail(to, emailType, language, verifyURL); err != nil {
			utils.LogError(logContext, "SendVerificationEmailAsync", err, fmt.Sprintf("to=%s, type=%s", to, emailType))
		}
	}()
}

// SendVerificationEmail 发送验证邮件（同步）
func (s *EmailService) SendVerificationEmail(to, emailType, language, verifyURL string) error {
	if to == "" {
		return ErrEmailEmptyRecipient
	}
	if verifyURL == "" {
		return errors.New("verify URL is empty")
	}

	s.mu.RLock()
	langTexts := s.getLanguageTexts(language)
	s.mu.RUnlock()

	common := langTexts["common"]
	typeTexts := s.getTypeTexts(langTexts, emailType)

	if err := validateTexts(common, typeTexts); err != nil {
		utils.LogWarn("EMAIL", "Missing texts, using default language", fmt.Sprintf("type=%s, language=%s", emailType, language))
		s.mu.RLock()
		langTexts = s.texts[defaultLanguage]
		s.mu.RUnlock()
		if langTexts != nil {
			common = langTexts["common"]
			typeTexts = s.getTypeTexts(langTexts, emailType)
		}
	}

	html := s.renderTemplate(common, typeTexts, verifyURL)

	textBody := s.renderTextBody(common, verifyURL)

	subject := typeTexts["subject"]
	if subject == "" {
		subject = "Verification Email"
		utils.LogWarn("EMAIL", "Missing subject, using default", fmt.Sprintf("type=%s", emailType))
	}

	if err := s.sendEmail(to, subject, html, textBody); err != nil {
		return fmt.Errorf("%w: %v", ErrEmailSendFailed, err)
	}

	return nil
}

// IsConfigured 检查邮件服务是否已配置
func (s *EmailService) IsConfigured() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	return s.cfg.SMTPHost != "" && s.cfg.SMTPUser != "" && s.cfg.SMTPPassword != ""
}

// getLanguageTexts 获取指定语言的文案
func (s *EmailService) getLanguageTexts(language string) map[string]map[string]string {
	if language == "" {
		language = defaultLanguage
	}

	langTexts, ok := s.texts[language]
	if !ok {
		utils.LogWarn("EMAIL", "Language not found, using default", fmt.Sprintf("language=%s, default=%s", language, defaultLanguage))
		langTexts = s.texts[defaultLanguage]
	}

	if langTexts == nil {
		utils.LogError("EMAIL", "getTexts", fmt.Errorf("default language texts not found"), "")
		return make(map[string]map[string]string)
	}

	return langTexts
}

// getTypeTexts 获取指定类型的文案
func (s *EmailService) getTypeTexts(langTexts map[string]map[string]string, emailType string) map[string]string {
	if emailType == "" {
		emailType = defaultEmailType
	}

	typeTexts, ok := langTexts[emailType]
	if !ok {
		utils.LogWarn("EMAIL", "Email type not found, using default", fmt.Sprintf("type=%s, default=%s", emailType, defaultEmailType))
		typeTexts = langTexts[defaultEmailType]
	}

	if typeTexts == nil {
		return make(map[string]string)
	}

	return typeTexts
}

// renderTemplate 渲染邮件模板
func (s *EmailService) renderTemplate(common, typeTexts map[string]string, verifyURL string) string {
	html := s.template

	html = strings.ReplaceAll(html, "{{PAGE_TITLE}}", safeGet(typeTexts, "pageTitle", "Verification"))
	html = strings.ReplaceAll(html, "{{DESCRIPTION}}", safeGet(typeTexts, "description", ""))

	html = strings.ReplaceAll(html, "{{GREETING}}", safeGet(common, "greeting", "Hello"))
	html = strings.ReplaceAll(html, "{{VERIFY_URL}}", verifyURL)
	html = strings.ReplaceAll(html, "{{BUTTON_TEXT}}", safeGet(common, "buttonText", "Verify"))
	html = strings.ReplaceAll(html, "{{LINK_HINT}}", safeGet(common, "linkHint", ""))
	html = strings.ReplaceAll(html, "{{EXPIRE_NOTICE}}", safeGet(common, "expireNotice", ""))
	html = strings.ReplaceAll(html, "{{SECURITY_TIP}}", safeGet(common, "securityTip", ""))
	html = strings.ReplaceAll(html, "{{FOOTER}}", safeGet(common, "footer", ""))

	return html
}

// renderTextBody 渲染纯文本邮件内容
func (s *EmailService) renderTextBody(common map[string]string, verifyURL string) string {
	textBody := safeGet(common, "textBody", "Please verify your email: {{VERIFY_URL}}")
	return strings.ReplaceAll(textBody, "{{VERIFY_URL}}", verifyURL)
}

// sendEmail 发送邮件
func (s *EmailService) sendEmail(to, subject, htmlBody, textBody string) error {
	if to == "" {
		return ErrEmailEmptyRecipient
	}
	if subject == "" {
		return ErrEmailEmptySubject
	}

	client, err := s.getClient()
	if err != nil {
		return err
	}

	msg := mail.NewMsg()

	if err := msg.From(s.cfg.SMTPFrom); err != nil {
		utils.LogError("EMAIL", "send", err, "Failed to set from address")
		return fmt.Errorf("failed to set from address: %w", err)
	}

	if err := msg.To(to); err != nil {
		utils.LogError("EMAIL", "send", err, "Failed to set to address")
		return fmt.Errorf("failed to set to address: %w", err)
	}

	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextPlain, textBody)
	msg.AddAlternativeString(mail.TypeTextHTML, htmlBody)

	if err := client.DialAndSend(msg); err != nil {
		utils.LogError("EMAIL", "send", err, fmt.Sprintf("Failed to send email: to=%s, subject=%s", to, subject))
		// 发送失败，重置连接（下次会重新建立）
		s.resetClient()
		return fmt.Errorf("failed to send email: %w", err)
	}

	utils.LogInfo("EMAIL", fmt.Sprintf("Email sent successfully: to=%s, subject=%s", to, subject))
	return nil
}

// createClient 创建 SMTP 客户端
func (s *EmailService) createClient() (*mail.Client, error) {
	if s.cfg == nil {
		return nil, ErrEmailNilConfig
	}

	var tlsPolicy mail.TLSPolicy
	var authType mail.SMTPAuthType
	var useSSL bool

	switch s.cfg.SMTPPort {
	case smtpPort465:
		tlsPolicy = mail.TLSMandatory
		authType = mail.SMTPAuthLogin // 网易邮箱需要 LOGIN 认证
		useSSL = true
	case smtpPort587:
		tlsPolicy = mail.TLSOpportunistic
		authType = mail.SMTPAuthPlain
		useSSL = false
	default:
		// 其他端口默认使用 STARTTLS
		tlsPolicy = mail.TLSOpportunistic
		authType = mail.SMTPAuthPlain
		useSSL = false
		utils.LogWarn("EMAIL", "Non-standard SMTP port, using STARTTLS", fmt.Sprintf("port=%d", s.cfg.SMTPPort))
	}

	options := []mail.Option{
		mail.WithPort(s.cfg.SMTPPort),
		mail.WithSMTPAuth(authType),
		mail.WithUsername(s.cfg.SMTPUser),
		mail.WithPassword(s.cfg.SMTPPassword),
		mail.WithTLSPortPolicy(tlsPolicy),
		mail.WithTimeout(smtpTimeout),
	}

	if useSSL {
		options = append(options, mail.WithSSL())
	}

	client, err := mail.NewClient(s.cfg.SMTPHost, options...)
	if err != nil {
		utils.LogError("EMAIL", "createClient", err, fmt.Sprintf("Failed to create SMTP client: host=%s, port=%d",
			s.cfg.SMTPHost, s.cfg.SMTPPort))
		return nil, fmt.Errorf("%w: %v", ErrEmailClientCreateFailed, err)
	}

	return client, nil
}

// getClient 获取或创建 SMTP 客户端（连接池）
func (s *EmailService) getClient() (*mail.Client, error) {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()

	if s.client != nil {
		s.lastUsed = time.Now()
		return s.client, nil
	}

	client, err := s.createClient()
	if err != nil {
		return nil, err
	}

	s.client = client
	s.lastUsed = time.Now()
	utils.LogInfo("EMAIL", "SMTP connection established (pooled)")

	return s.client, nil
}

// closeClient 关闭当前连接
func (s *EmailService) closeClient() {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()

	if s.client != nil {
		if err := s.client.Close(); err != nil {
			utils.LogWarn("EMAIL", "Failed to close SMTP client", "")
		}
		s.client = nil
		utils.LogInfo("EMAIL", "SMTP connection closed")
	}
}

// resetClient 重置连接（发送失败时调用）
func (s *EmailService) resetClient() {
	s.clientMu.Lock()
	defer s.clientMu.Unlock()

	if s.client != nil {
		_ = s.client.Close()
		s.client = nil
	}
}

// connectionKeeper 连接保活协程
// 定期检查连接状态，关闭空闲过久的连接
func (s *EmailService) connectionKeeper() {
	ticker := time.NewTicker(connCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.clientMu.Lock()
			if s.client != nil && time.Since(s.lastUsed) > connMaxIdleTime {
				_ = s.client.Close()
				s.client = nil
				utils.LogInfo("EMAIL", "SMTP connection closed due to idle timeout")
			}
			s.clientMu.Unlock()
		case <-s.stopKeeper:
			s.closeClient()
			return
		}
	}
}

// Close 关闭邮件服务
func (s *EmailService) Close() {
	if s.stopKeeper != nil {
		close(s.stopKeeper)
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		utils.LogInfo("EMAIL", "All inflight email sends completed")
	case <-time.After(30 * time.Second):
		utils.LogWarn("EMAIL", "Timed out waiting for inflight email sends")
	}
}

// validateSMTPConfig 验证 SMTP 配置
func validateSMTPConfig(cfg *config.Config) error {
	if cfg.SMTPHost == "" {
		return fmt.Errorf("%w: SMTP host is empty", ErrEmailSMTPConfigMissing)
	}
	if cfg.SMTPPort == 0 {
		return fmt.Errorf("%w: SMTP port is 0", ErrEmailSMTPConfigMissing)
	}
	if cfg.SMTPUser == "" {
		return fmt.Errorf("%w: SMTP user is empty", ErrEmailSMTPConfigMissing)
	}
	if cfg.SMTPPassword == "" {
		return fmt.Errorf("%w: SMTP password is empty", ErrEmailSMTPConfigMissing)
	}
	if cfg.SMTPFrom == "" {
		return fmt.Errorf("%w: SMTP from address is empty", ErrEmailSMTPConfigMissing)
	}
	return nil
}

// loadTemplate 加载邮件模板
func loadTemplate(path string) (string, error) {
	templateBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%w: %s", ErrEmailTemplateNotFound, path)
		}
		return "", fmt.Errorf("failed to read email template: %w", err)
	}

	template := string(templateBytes)
	if template == "" {
		return "", fmt.Errorf("%w: template is empty", ErrEmailTemplateNotFound)
	}

	utils.LogInfo("EMAIL", fmt.Sprintf("Email template loaded: %s (%d bytes)", path, len(templateBytes)))
	return template, nil
}

// loadTexts 加载多语言文案
func loadTexts(path string) (EmailTexts, error) {
	textsBytes, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrEmailTextsNotFound, path)
		}
		return nil, fmt.Errorf("failed to read email texts: %w", err)
	}

	var texts EmailTexts
	if err := json.Unmarshal(textsBytes, &texts); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmailInvalidTexts, err)
	}

	if len(texts) == 0 {
		return nil, fmt.Errorf("%w: texts is empty", ErrEmailInvalidTexts)
	}

	utils.LogInfo("EMAIL", fmt.Sprintf("Email texts loaded: %s (%d languages)", path, len(texts)))
	return texts, nil
}

// validateTemplateAndTexts 验证模板和文案
func validateTemplateAndTexts(template string, texts EmailTexts) error {
	var missingPlaceholders []string
	for _, placeholder := range templatePlaceholders {
		if !strings.Contains(template, placeholder) {
			missingPlaceholders = append(missingPlaceholders, placeholder)
		}
	}

	if len(missingPlaceholders) > 0 {
		return fmt.Errorf("template missing placeholders: %v", missingPlaceholders)
	}

	if _, ok := texts[defaultLanguage]; !ok {
		return fmt.Errorf("default language %s not found in texts", defaultLanguage)
	}

	return nil
}

// validateTexts 验证文案是否完整
func validateTexts(common, typeTexts map[string]string) error {
	if common == nil {
		return errors.New("common texts is nil")
	}
	if typeTexts == nil {
		return errors.New("type texts is nil")
	}

	requiredCommon := []string{"greeting", "buttonText"}
	for _, key := range requiredCommon {
		if common[key] == "" {
			return fmt.Errorf("missing common text: %s", key)
		}
	}

	requiredType := []string{"subject", "pageTitle"}
	for _, key := range requiredType {
		if typeTexts[key] == "" {
			return fmt.Errorf("missing type text: %s", key)
		}
	}

	return nil
}

// safeGet 安全获取 map 值
func safeGet(m map[string]string, key, defaultValue string) string {
	if m == nil {
		return defaultValue
	}
	if v, ok := m[key]; ok && v != "" {
		return v
	}
	return defaultValue
}
