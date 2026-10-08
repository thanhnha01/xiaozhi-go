# Giao diện XiaoZhi PC tiếng Việt

Nhấp đúp `XiaoZhi-PC.exe` để mở **cửa sổ ứng dụng Windows bằng WebView2**, không cần CMD hay tab trình duyệt. Giao diện kết nối tới dịch vụ nền tại `127.0.0.1` chỉ trong máy tính.

- **Bắt đầu nói**: Bật microphone, nói yêu cầu; **Dừng nói**: kết thúc lượt nói. Không có giới hạn 10 giây trong chế độ mặc định, nên bạn phải nhấn Dừng nói để gửi kết thúc lượt.
- **Giám sát mic**: Biểu đồ mức âm và số khung Opus đã gửi thành công lên WebSocket sẽ tăng khi đang nói. Số khung gửi thành công không xác nhận máy chủ đã nhận dạng được lời nói; kiểm tra thêm STT/LLM và log Xiaozhi.
- **Âm lượng**: từ 0 đến 100%, mặc định 70%. Đây là mức âm lượng phát trong ứng dụng, không tăng gain microphone hay thay đổi âm lượng hệ thống Windows.
- **Ngắt phản hồi**: Dừng câu trả lời đang phát.
- **Liên kết thiết bị**: Hiển thị mã kích hoạt 6 chữ số khi xiaozhi.me yêu cầu. Bấm liên kết xiaozhi.me trong giao diện, nhập mã để ghép thiết bị.
- **Lịch sử hội thoại**: Hiển thị STT (lời bạn nói), LLM/phụ đề trợ lý và trạng thái hoạt động.
- **Nhật ký kỹ thuật**: Thu gọn sẵn; chỉ mở khi cần chẩn đoán lỗi.
- **Âm lượng**: Kéo thanh chỉnh trên giao diện.

Nếu máy tính thiếu **Microsoft Edge WebView2 Runtime**, hãy cài runtime từ Microsoft. Ứng dụng có thể thử chuyển sang trình duyệt khi không khởi tạo được WebView2. Đóng cửa sổ sẽ kết thúc chương trình và ngắt kết nối với máy chủ.

Mặc định dùng giao diện tiếng Việt. Bản Windows này được đóng gói ở chế độ `windowsgui` (không có CMD). Để dùng phím bấm dòng lệnh cũ, cần tự build một bản console với `go build -tags nolibopusfile -o xiaozhi-console.exe .` rồi chạy với `-console`. Tham số `-ws` chỉ nhận WebSocket **thiết bị**; không phải WSS endpoint của MCP server.

Giao diện chạy trong **cửa sổ native Win32/WebView2**. Nội dung HTML/CSS được nhúng trong EXE và API điều khiển chỉ lắng nghe trên `127.0.0.1`.

---

# XiaoZhi-Go — kiểm thử trên Windows 10/11 (x64)

## 1. Tải chương trình từ GitHub Actions

Vào repository **Actions** → **Build Windows XiaoZhi MCP Tester** → chọn run có dấu tích xanh → **Artifacts** → **xiaozhi-go-windows-x64**. Khi thay đổi đang nằm trên Pull Request, vào tab **Checks / Details** hoặc Actions của PR để tìm run. Workflow `workflow_dispatch` xuất hiện trên nhánh mặc định sau khi workflow được merge.

Giải nén ZIP vào một thư mục riêng và giữ `XiaoZhi-PC.exe`, `libopus-0.dll`, `libportaudio.dll` (cùng những DLL khác) chung một thư mục. Không tải riêng file EXE.

## 2. Chế độ mặc định — kích hoạt qua xiaozhi.me

Mở PowerShell trong thư mục giải nén:

```powershell
.\XiaoZhi-PC.exe -config .\device_config.json
```

Ứng dụng tạo MAC và UUID cố định trong `device_config.json`; nếu chưa có thiết bị trên tài khoản, màn hình sẽ hiện mã kích hoạt 6 chữ số. Đăng nhập **https://xiaozhi.me**, thêm thiết bị bằng mã này. Sau khi máy chủ xác nhận, ứng dụng kết nối WebSocket của thiết bị.

Không công khai `device_config.json`: file có thể chứa thông tin kết nối hoặc token của thiết bị.

## 3. Thử MCP Server đang nối với Xiaozhi

1. Trên bảng điều khiển Xiaozhi, đảm bảo MCP Server Cloudflare Workers/Render của bạn đã kết nối và các công cụ đã được nhận diện.
2. Trên PC cấp quyền microphone; khởi chạy EXE và chờ dòng xác nhận kết nối.
3. Bấm `1` để bắt đầu thu âm; nói câu hỏi tương ứng công cụ, ví dụ: "Thời tiết hôm nay ở Ngũ Hành Sơn, Đà Nẵng thế nào?". Bấm `2` để kết thúc lượt nói.
4. Quan sát chữ STT / LLM trên cửa sổ ứng dụng, nghe âm thanh phản hồi và kiểm tra log từ Worker/Render hoặc bảng điều khiển Xiaozhi để xác nhận công cụ thực sự được gọi.
5. Phím `5` chỉ hiển thị thông tin chẩn đoán; không tự gọi MCP Server. Phím `6` thoát.

**Luồng đúng:** App PC → WSS thiết bị Xiaozhi → backend Xiaozhi → MCP Server riêng của bạn. Không nhập WSS endpoint của MCP Server vào tùy chọn `-ws`.

## 4. Chế độ nâng cao: trực tiếp đến WebSocket thiết bị

Chỉ dùng nếu **đã có URL WebSocket cho thiết bị** và server cho phép cách này (không phải MCP endpoint):

```powershell
$env:XIAOZHI_WS_TOKEN = "DEVICE_WS_TOKEN"
.\XiaoZhi-PC.exe -ws "wss://your-device-websocket/path" -ws-version 1
```

`-ws` bỏ qua kích hoạt OTA trong lần chạy đó. Để trở về đăng ký qua Xiaozhi, chạy lại không có `-ws`. Không commit token vào GitHub. Nếu token được cấp từ OTA, nên để chế độ mặc định tự xử lý.

## 5. Lưu ý / xử lý sự cố

- Thiếu DLL: giải nén toàn bộ artifact ZIP, không chuyển chỉ EXE.
- Lỗi khởi tạo PortAudio: kiểm tra quyền microphone, thiết bị thu/phát âm thanh và chương trình đang chiếm microphone.
- Lỗi xác thực kết nối: kiểm tra trạng thái kích hoạt và URL thuộc giao thức **device WebSocket**, không phải MCP.
- Máy chủ trả lời nhưng không dùng tool: kiểm tra tool đã được khai báo, gắn với đúng thiết bị/agent trong bảng điều khiển và kiểm tra log MCP backend.
- Đây là **ứng dụng console với audio thật**, chưa phải ứng dụng GUI và không mô phỏng phần cứng ESP32 ở cấp firmware.
