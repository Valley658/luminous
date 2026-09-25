// Package email은 비밀번호 재설정 등에 필요한 트랜잭션 메일을 SMTP로 보낸다.
// 외부 이메일 서비스 SDK 없이 표준 라이브러리(net/smtp)만 쓰는 가장 단순한
// 구현 - Gmail(앱 비밀번호), Naver, 사내/호스팅 제공 SMTP 등 TLS(STARTTLS)를
// 지원하는 표준 SMTP 서버라면 대부분 그대로 동작한다.
package email

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

type Client struct {
	host     string
	port     int
	user     string
	pass     string
	from     string
	fromName string
}

// New는 SMTP 설정이 비어있으면(host나 from이 없으면) nil을 돌려준다 -
// 호출하는 쪽에서 nil 체크만으로 "이메일 기능 꺼짐"을 자연스럽게 처리할 수
// 있게 하기 위함(리도님이 SMTP를 아직 설정 안 했어도 서버가 죽지 않음).
func New(host string, port int, user, pass, from, fromName string) *Client {
	if strings.TrimSpace(host) == "" || strings.TrimSpace(from) == "" {
		return nil
	}
	return &Client{host: host, port: port, user: user, pass: pass, from: from, fromName: fromName}
}

func (c *Client) Enabled() bool { return c != nil }

// Send는 to 한 명에게 HTML 본문의 메일을 보낸다. STARTTLS를 우선 시도하고
// (587 포트 관례), 465처럼 처음부터 TLS로 붙는 포트면 바로 TLS 연결한다.
func (c *Client) Send(to, subject, htmlBody string) error {
	if c == nil {
		return fmt.Errorf("이메일 기능이 설정되지 않았습니다")
	}
	addr := fmt.Sprintf("%s:%d", c.host, c.port)

	fromHeader := c.from
	if c.fromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", c.fromName, c.from)
	}
	msg := buildMessage(fromHeader, to, subject, htmlBody)

	var auth smtp.Auth
	if c.user != "" {
		auth = smtp.PlainAuth("", c.user, c.pass, c.host)
	}

	// 465(SMTPS, implicit TLS)는 net/smtp.SendMail이 처리 못 하는 옛날 방식이라
	// 별도 경로가 필요하다. 그 외(587/25 등)는 SendMail이 알아서 STARTTLS를
	// 협상한다.
	if c.port == 465 {
		return c.sendImplicitTLS(addr, auth, to, msg)
	}
	return smtp.SendMail(addr, auth, c.from, []string{to}, msg)
}

func (c *Client) sendImplicitTLS(addr string, auth smtp.Auth, to string, msg []byte) error {
	tlsConn, err := tls.Dial("tcp", addr, &tls.Config{ServerName: c.host})
	if err != nil {
		return err
	}
	defer tlsConn.Close()

	client, err := smtp.NewClient(tlsConn, c.host)
	if err != nil {
		return err
	}
	defer client.Close()

	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(c.from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func buildMessage(from, to, subject, htmlBody string) []byte {
	var sb strings.Builder
	sb.WriteString("From: " + from + "\r\n")
	sb.WriteString("To: " + to + "\r\n")
	sb.WriteString("Subject: " + mimeEncodeSubject(subject) + "\r\n")
	sb.WriteString("MIME-Version: 1.0\r\n")
	sb.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	sb.WriteString("\r\n")
	sb.WriteString(htmlBody)
	return []byte(sb.String())
}

// mimeEncodeSubject는 제목에 한글이 섞여도 이메일 클라이언트가 깨지지 않게
// RFC 2047 인코딩(Base64)으로 감싼다.
func mimeEncodeSubject(subject string) string {
	if isASCII(subject) {
		return subject
	}
	return "=?UTF-8?B?" + base64Encode(subject) + "?="
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 127 {
			return false
		}
	}
	return true
}

func base64Encode(s string) string {
	const tbl = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	b := []byte(s)
	var sb strings.Builder
	for i := 0; i < len(b); i += 3 {
		var chunk [3]byte
		n := copy(chunk[:], b[i:min(i+3, len(b))])
		sb.WriteByte(tbl[chunk[0]>>2])
		sb.WriteByte(tbl[(chunk[0]&0x03)<<4|chunk[1]>>4])
		if n > 1 {
			sb.WriteByte(tbl[(chunk[1]&0x0f)<<2|chunk[2]>>6])
		} else {
			sb.WriteByte('=')
		}
		if n > 2 {
			sb.WriteByte(tbl[chunk[2]&0x3f])
		} else {
			sb.WriteByte('=')
		}
	}
	return sb.String()
}
