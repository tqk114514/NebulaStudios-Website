/**
 * internal/services/email.go
 * 邮件服务
 *
 * 功能：
 * - SMTP 连接管理
 * - HTML 邮件模板渲染
 * - 多语言支持（zh-CN, en, ja, ko 等）
 * - 验证邮件发送
 *
 * 依赖：
 * - github.com/wneessen/go-mail: SMTP 客户端
 * - Config: SMTP 配置
 * - dist/data/email-template.html: 邮件模板
 * - dist/data/email-texts.json: 多语言文案
 */

package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"auth-system/internal/config"

	"github.com/wneessen/go-mail"
)

// ====================  错误定义 ====================

var (
	// ErrEmailNilConfig 配置为空
	ErrEmailNilConfig = errors.New("email config is nil")
	// ErrEmailTemplateNotFound 模板文件未找到
	ErrEmailTemplateNotFound = errors.New("email template not found")
	// ErrEmailTextsNotFound 文案文件未找到
	ErrEmailTextsNotFound = errors.New("email texts not found")
	// ErrEmailInvalidTexts 无效的文案格式
	ErrEmailInvalidTexts = errors.New("invalid email texts format")
	// ErrEmailEmptyRecipient 收件人为空
	ErrEmailEmptyRecipient = errors.New("email recipient is empty")
	// ErrEmailEmptySubject 主题为空
	ErrEmailEmptySubject = errors.New("email subject is empty")
	// ErrEmailSMTPConfigMissing SMTP 配置缺失
	ErrEmailSMTPConfigMissing = errors.New("SMTP configuration is missing")
	// ErrEmailClientCreateFailed 客户端创建失败
	ErrEmailClientCreateFailed = errors.New("failed to create SMTP client")
	// ErrEmailSendFailed 发送失败
	ErrEmailSendFailed = errors.New("failed to send email")
)

// ====================  常量定义 ====================

const (
	// defaultLanguage 默认语言
	defaultLanguage = "zh-CN"

	// defaultEmailType 默认邮件类型
	defaultEmailType = "register"

	// smtpTimeout SMTP 超时时间
	smtpTimeout = 15 * time.Second

	// smtpPort465 SSL 端口
	smtpPort465 = 465

	// smtpPort587 STARTTLS 端口
	smtpPort587 = 587

	// templatePath 模板文件路径
	templatePath = "dist/data/email-template.html"

	// textsPath 文案文件路径
	textsPath = "dist/data/email-texts.json"
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

// ====================  数据结构 ====================

// EmailTexts 邮件文案
// 结构：language -> section -> key -> value
type EmailTexts map[string]map[string]map[string]string

// EmailService 邮件服务
type EmailService struct {
	cfg      *config.Config
	template string
	texts    EmailTexts
	mu       sync.RWMutex
}

// ====================  构造函数 ====================

// NewEmailService 创建邮件服务
// 参数：
//   - cfg: 应用配置
//
// 返回：
//   - *EmailService: 邮件服务实例
//   - error: 错误信息
func NewEmailService(cfg *config.Config) (*EmailService, error) {
	// 参数验证
	if cfg == nil {
		return nil, ErrEmailNilConfig
	}

	// 验证 SMTP 配置
	if err := validateSMTPConfig(cfg); err != nil {
		return nil, err
	}

	// 加载 HTML 模板
	template, err := loadTemplate(templatePath)
	if err != nil {
		return nil, err
	}

	// 加载多语言文案
	texts, err := loadTexts(textsPath)
	if err != nil {
		return nil, err
	}

	// 验证模板和文案
	if err := validateTemplateAndTexts(template, texts); err != nil {
		log.Printf("[EMAIL] WARN: Template validation warning: %v", err)
		// 不返回错误，只记录警告
	}

	log.Printf("[EMAIL] Email service initialized: host=%s, port=%d, from=%s",
		cfg.SMTPHost, cfg.SMTPPort, cfg.SMTPFrom)

	return &EmailService{
		cfg:      cfg,
		template: template,
		texts:    texts,
	}, nil
}

// ====================  公开方法 ====================

// VerifyConnection 验证 SMTP 连接
// 返回：
//   - error: 连接失败时返回错误
func (s *EmailService) VerifyConnection() error {
	if s == nil {
		return errors.New("email service is nil")
	}

	client, err := s.createClient()
	if err != nil {
		log.Printf("[EMAIL] ERROR: SMTP connection verification failed: %v", err)
		return fmt.Errorf("SMTP connection failed: %w", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			log.Printf("[EMAIL] WARN: Failed to close SMTP client: %v", err)
		}
	}()

	log.Println("[EMAIL] SMTP connection verified successfully")
	return nil
}

// SendVerificationEmail 发送验证邮件
// 参数：
//   - to: 收件人邮箱
//   - emailType: 邮件类型（register, reset_password, delete_account 等）
//   - language: 语言代码（zh-CN, en, ja 等）
//   - verifyURL: 验证链接
//
// 返回：
//   - error: 发送失败时返回错误
func (s *EmailService) SendVerificationEmail(to, emailType, language, verifyURL string) error {
	// 参数验证
	if to == "" {
		return ErrEmailEmptyRecipient
	}
	if verifyURL == "" {
		return errors.New("verify URL is empty")
	}

	// 获取语言文案
	s.mu.RLock()
	langTexts := s.getLanguageTexts(language)
	s.mu.RUnlock()

	// 获取通用文案和类型文案
	common := langTexts["common"]
	typeTexts := s.getTypeTexts(langTexts, emailType)

	// 验证必要的文案是否存在
	if err := validateTexts(common, typeTexts); err != nil {
		log.Printf("[EMAIL] WARN: Missing texts for type=%s, language=%s: %v", emailType, language, err)
		// 使用默认语言重试
		s.mu.RLock()
		langTexts = s.texts[defaultLanguage]
		s.mu.RUnlock()
		if langTexts != nil {
			common = langTexts["common"]
			typeTexts = s.getTypeTexts(langTexts, emailType)
		}
	}

	// 渲染 HTML
	html := s.renderTemplate(common, typeTexts, verifyURL)

	// 纯文本版本
	textBody := s.renderTextBody(common, verifyURL)

	// 获取主题
	subject := typeTexts["subject"]
	if subject == "" {
		subject = "Verification Email"
		log.Printf("[EMAIL] WARN: Missing subject for type=%s, using default", emailType)
	}

	// 发送邮件
	if err := s.sendEmail(to, subject, html, textBody); err != nil {
		return fmt.Errorf("%w: %v", ErrEmailSendFailed, err)
	}

	return nil
}

// IsConfigured 检查邮件服务是否已配置
// 返回：
//   - bool: 是否已配置
func (s *EmailService) IsConfigured() bool {
	if s == nil || s.cfg == nil {
		return false
	}
	return s.cfg.SMTPHost != "" && s.cfg.SMTPUser != "" && s.cfg.SMTPPassword != ""
}

// ====================  私有方法 ====================

// getLanguageTexts 获取指定语言的文案
// 参数：
//   - language: 语言代码
//
// 返回：
//   - map[string]map[string]string: 语言文案
func (s *EmailService) getLanguageTexts(language string) map[string]map[string]string {
	if language == "" {
		language = defaultLanguage
	}

	langTexts, ok := s.texts[language]
	if !ok {
		log.Printf("[EMAIL] WARN: Language not found: %s, using default %s", language, defaultLanguage)
		langTexts = s.texts[defaultLanguage]
	}

	if langTexts == nil {
		log.Printf("[EMAIL] ERROR: Default language texts not found")
		return make(map[string]map[string]string)
	}

	return langTexts
}

// getTypeTexts 获取指定类型的文案
// 参数：
//   - langTexts: 语言文案
//   - emailType: 邮件类型
//
// 返回：
//   - map[string]string: 类型文案
func (s *EmailService) getTypeTexts(langTexts map[string]map[string]string, emailType string) map[string]string {
	if emailType == "" {
		emailType = defaultEmailType
	}

	typeTexts, ok := langTexts[emailType]
	if !ok {
		log.Printf("[EMAIL] WARN: Email type not found: %s, using default %s", emailType, defaultEmailType)
		typeTexts = langTexts[defaultEmailType]
	}

	if typeTexts == nil {
		return make(map[string]string)
	}

	return typeTexts
}

// renderTemplate 渲染邮件模板
// 参数：
//   - common: 通用文案
//   - typeTexts: 类型文案
//   - verifyURL: 验证链接
//
// 返回：
//   - string: 渲染后的 HTML
func (s *EmailService) renderTemplate(common, typeTexts map[string]string, verifyURL string) string {
	html := s.template

	// 替换类型相关占位符
	html = strings.ReplaceAll(html, "{{PAGE_TITLE}}", safeGet(typeTexts, "pageTitle", "Verification"))
	html = strings.ReplaceAll(html, "{{DESCRIPTION}}", safeGet(typeTexts, "description", ""))

	// 替换通用占位符
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
// 参数：
//   - common: 通用文案
//   - verifyURL: 验证链接
//
// 返回：
//   - string: 纯文本内容
func (s *EmailService) renderTextBody(common map[string]string, verifyURL string) string {
	textBody := safeGet(common, "textBody", "Please verify your email: {{VERIFY_URL}}")
	return strings.ReplaceAll(textBody, "{{VERIFY_URL}}", verifyURL)
}

// sendEmail 发送邮件
// 参数：
//   - to: 收件人
//   - subject: 主题
//   - htmlBody: HTML 内容
//   - textBody: 纯文本内容
//
// 返回：
//   - error: 发送失败时返回错误
func (s *EmailService) sendEmail(to, subject, htmlBody, textBody string) error {
	// 参数验证
	if to == "" {
		return ErrEmailEmptyRecipient
	}
	if subject == "" {
		return ErrEmailEmptySubject
	}

	// 创建客户端
	client, err := s.createClient()
	if err != nil {
		return err
	}

	// 创建邮件消息
	msg := mail.NewMsg()

	// 设置发件人
	if err := msg.From(s.cfg.SMTPFrom); err != nil {
		log.Printf("[EMAIL] ERROR: Failed to set from address: %v", err)
		return fmt.Errorf("failed to set from address: %w", err)
	}

	// 设置收件人
	if err := msg.To(to); err != nil {
		log.Printf("[EMAIL] ERROR: Failed to set to address: %v", err)
		return fmt.Errorf("failed to set to address: %w", err)
	}

	// 设置主题和内容
	msg.Subject(subject)
	msg.SetBodyString(mail.TypeTextPlain, textBody)
	msg.AddAlternativeString(mail.TypeTextHTML, htmlBody)

	// 发送邮件
	if err := client.DialAndSend(msg); err != nil {
		log.Printf("[EMAIL] ERROR: Failed to send email: to=%s, subject=%s, error=%v", to, subject, err)
		return fmt.Errorf("failed to send email: %w", err)
	}

	log.Printf("[EMAIL] Email sent successfully: to=%s, subject=%s", to, subject)
	return nil
}

// createClient 创建 SMTP 客户端
// 返回：
//   - *mail.Client: SMTP 客户端
//   - error: 创建失败时返回错误
func (s *EmailService) createClient() (*mail.Client, error) {
	// 检查配置
	if s.cfg == nil {
		return nil, ErrEmailNilConfig
	}

	// 根据端口选择 TLS 策略和认证方式
	// 465: 直接 SSL，网易邮箱用 LOGIN 认证
	// 587: STARTTLS，使用 PLAIN 认证
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
		log.Printf("[EMAIL] WARN: Non-standard SMTP port %d, using STARTTLS", s.cfg.SMTPPort)
	}

	// 构建客户端选项
	options := []mail.Option{
		mail.WithPort(s.cfg.SMTPPort),
		mail.WithSMTPAuth(authType),
		mail.WithUsername(s.cfg.SMTPUser),
		mail.WithPassword(s.cfg.SMTPPassword),
		mail.WithTLSPortPolicy(tlsPolicy),
		mail.WithTimeout(smtpTimeout),
	}

	// 465 端口需要直接 SSL
	if useSSL {
		options = append(options, mail.WithSSL())
	}

	// 创建客户端
	client, err := mail.NewClient(s.cfg.SMTPHost, options...)
	if err != nil {
		log.Printf("[EMAIL] ERROR: Failed to create SMTP client: host=%s, port=%d, error=%v",
			s.cfg.SMTPHost, s.cfg.SMTPPort, err)
		return nil, fmt.Errorf("%w: %v", ErrEmailClientCreateFailed, err)
	}

	return client, nil
}

// ====================  辅助函数 ====================

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

	log.Printf("[EMAIL] Email template loaded: %s (%d bytes)", path, len(templateBytes))
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

	log.Printf("[EMAIL] Email texts loaded: %s (%d languages)", path, len(texts))
	return texts, nil
}

// validateTemplateAndTexts 验证模板和文案
func validateTemplateAndTexts(template string, texts EmailTexts) error {
	// 检查模板是否包含必要的占位符
	var missingPlaceholders []string
	for _, placeholder := range templatePlaceholders {
		if !strings.Contains(template, placeholder) {
			missingPlaceholders = append(missingPlaceholders, placeholder)
		}
	}

	if len(missingPlaceholders) > 0 {
		return fmt.Errorf("template missing placeholders: %v", missingPlaceholders)
	}

	// 检查默认语言是否存在
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

	// 检查必要的字段
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
