package main

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "net/http"
 "net/url"
 "strings"
 "time"
)

const inboxMaxRunes = 2000

// ChatInboxClient is an opt-in client for the experimental Cloudflare MCP
// inbox relay. It intentionally never exposes the bearer token via /api/status.
type ChatInboxClient struct {
 BaseURL string
 Token string
 HTTP *http.Client
}

type inboxResponse struct {
 OK bool `json:"ok"`
 Code string `json:"code"`
 Error json.RawMessage `json:"error"`
}
type inboxStatusResponse struct {
 OK bool `json:"ok"`
 Status string `json:"status"`
}

func (c *ChatInboxClient) Validate() error {
 if c==nil || strings.TrimSpace(c.BaseURL)=="" || strings.TrimSpace(c.Token)=="" {
  return errors.New("MCP Inbox chưa được cấu hình. Cần XIAOZHI_CHAT_INBOX_URL và XIAOZHI_CHAT_INBOX_TOKEN")
 }
 u,err:=url.Parse(c.BaseURL)
 if err!=nil||u.Scheme!="https"||u.Host==""||u.User!=nil||u.RawQuery!=""||u.Fragment!="" {
  return errors.New("URL MCP Inbox phải là HTTPS hợp lệ và không chứa token")
 }
 return nil
}

func (c *ChatInboxClient) client() *http.Client {
 if c.HTTP!=nil{return c.HTTP}
 return &http.Client{Timeout:12*time.Second}
}

func (c *ChatInboxClient) Submit(ctx context.Context,text string)(string,error){
 if err:=c.Validate();err!=nil{return "",err}
 raw,err:=json.Marshal(map[string]string{"text":text})
 if err!=nil{return "",err}
 req,err:=http.NewRequestWithContext(ctx,http.MethodPost,strings.TrimRight(c.BaseURL,"/")+"/api/chat/inbox",bytes.NewReader(raw))
 if err!=nil{return "",err}
 req.Header.Set("Content-Type","application/json")
 req.Header.Set("Authorization","Bearer "+c.Token)
 resp,err:=c.client().Do(req)
 if err!=nil{return "",fmt.Errorf("không gửi được đến MCP Inbox: %w",err)}
 defer resp.Body.Close()
 data,err:=io.ReadAll(io.LimitReader(resp.Body,4096))
 if err!=nil{return "",err}
 var out inboxResponse
 if json.Unmarshal(data,&out)!=nil{return "",fmt.Errorf("Worker trả dữ liệu không hợp lệ (HTTP %d)",resp.StatusCode)}
 if resp.StatusCode!=http.StatusOK||!out.OK{return "",fmt.Errorf("MCP Inbox lỗi HTTP %d: %s",resp.StatusCode,readInboxError(out.Error))}
 if len(out.Code)<4||len(out.Code)>12{return "",errors.New("Worker không trả mã yêu cầu hợp lệ")}
 for _,ch:=range out.Code {if !(ch>='A'&&ch<='Z'||ch>='0'&&ch<='9'){return "",errors.New("Mã yêu cầu chứa ký tự không hợp lệ")}}
 return out.Code,nil
}

func readInboxError(raw json.RawMessage) string{
 if len(raw)==0{return "không rõ nguyên nhân"}
 var msg string
 if json.Unmarshal(raw,&msg)==nil{return msg}
 var obj struct {Message string `json:"message"`;Code string `json:"code"`}
 if json.Unmarshal(raw,&obj)==nil&&obj.Message!="" {return obj.Message}
 return "không rõ nguyên nhân"
}

func(c *ChatInboxClient) Check(ctx context.Context,code string)(string,error){
 if err:=c.Validate();err!=nil{return "",err}
 u:=strings.TrimRight(c.BaseURL,"/")+"/api/chat/status?code="+url.QueryEscape(code)
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,u,nil)
 if err!=nil{return "",err}
 req.Header.Set("Authorization","Bearer "+c.Token)
 resp,err:=c.client().Do(req)
 if err!=nil{return "",err}
 defer resp.Body.Close()
 if resp.StatusCode!=http.StatusOK{return "",fmt.Errorf("kiểm tra Inbox: HTTP %d",resp.StatusCode)}
 var value inboxStatusResponse
 if err=json.NewDecoder(io.LimitReader(resp.Body,2048)).Decode(&value);err!=nil{return "",err}
 if !value.OK{return "",errors.New("không đọc được trạng thái Inbox")}
 return value.Status,nil
}
