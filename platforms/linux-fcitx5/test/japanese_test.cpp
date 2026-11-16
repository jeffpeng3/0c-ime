// 日文契約（T15）在 C++ session 的解讀與能力旗標：不需要 Fcitx，使用假的 unix socket server。
#include "../src/session.h"

#include <cstdio>
#include <cstring>
#include <string>
#include <thread>
#include <sys/socket.h>
#include <sys/un.h>
#include <unistd.h>

using nlohmann::json;

static int failures = 0;
#define EXPECT(cond) do { if (!(cond)) { std::fprintf(stderr, "FAIL %s:%d %s\n", __FILE__, __LINE__, #cond); ++failures; } } while (0)

static bool transfer(int fd, void *data, size_t n, bool write) {
    auto *p = static_cast<char *>(data);
    while (n) {
        auto got = write ? ::write(fd, p, n) : ::read(fd, p, n);
        if (got <= 0) { return false; }
        p += got;
        n -= static_cast<size_t>(got);
    }
    return true;
}

static bool receive(int fd, json &j) {
    unsigned char h[4];
    if (!transfer(fd, h, 4, false)) { return false; }
    const uint32_t n = h[0] | h[1] << 8 | h[2] << 16 | h[3] << 24;
    if (n >= 65536) { return false; }
    std::string body(n, '\0');
    if (!transfer(fd, body.data(), n, false)) { return false; }
    j = json::parse(body);
    return true;
}

static void send(int fd, const json &j) {
    const auto body = j.dump();
    const uint32_t n = static_cast<uint32_t>(body.size());
    unsigned char h[4] = {static_cast<unsigned char>(n), static_cast<unsigned char>(n >> 8),
                          static_cast<unsigned char>(n >> 16), static_cast<unsigned char>(n >> 24)};
    transfer(fd, h, 4, true);
    transfer(fd, const_cast<char *>(body.data()), body.size(), true);
}

// run 起一個只服務一個連線的假引擎。japanese 為 true 時回報日文能力與日文快照；
// false 時模擬舊引擎：沒有能力旗標、照舊回注音快照。
static void run(bool japanese) {
    const std::string path = "/tmp/0c-ja-test-" + std::to_string(getpid()) + (japanese ? "-ja.sock" : "-legacy.sock");
    unlink(path.c_str());
    const int listener = socket(AF_UNIX, SOCK_STREAM, 0);
    EXPECT(listener >= 0);
    sockaddr_un address{};
    address.sun_family = AF_UNIX;
    std::strcpy(address.sun_path, path.c_str());
    EXPECT(bind(listener, reinterpret_cast<sockaddr *>(&address), sizeof(address)) == 0);
    EXPECT(listen(listener, 1) == 0);
    std::thread server([&] {
        const int fd = accept(listener, nullptr, nullptr);
        if (fd < 0) { return; }
        json request;
        while (receive(fd, request)) {
            const auto op = request.value("op", "");
            if (op == "open") {
                json info = {{"version", "test"}};
                if (japanese) {
                    info["japanese"] = "true";
                    info["convert_keys"] = "true";
                    info["clause_display"] = "true";
                }
                send(fd, {{"ok", true}, {"session", 1}, {"info", info}});
                continue;
            }
            if (op == "close") {
                send(fd, {{"ok", true}});
                break;
            }
            json snap = {{"handled", true}, {"revision", 1}, {"cursor", {{"rune", 0}, {"utf16", 0}}}};
            if (japanese) {
                snap["mode"] = "japanese";
                snap["language"] = "ja";
                snap["input_method"] = "romaji";
                snap["converting"] = true;
                snap["clause_index"] = 1;
                snap["clause_count"] = 2;
                snap["preedit"] = {{{"text", "今日"}, {"style", "clause"}},
                                   {{"text", "は"}, {"style", "clause_active"}}};
                snap["cursor"] = {{"rune", 3}, {"utf16", 3}};
                snap["candidates"] = {{"items", {{{"id", 0}, {"text", "今日"}, {"key", "1"}}}},
                                      {"page", 0}, {"pages", 1}, {"highlight", 0}, {"title", "変換"}};
            } else {
                snap["mode"] = "zhuyin";
                snap["preedit"] = {{{"text", "我"}, {"style", "normal"}}};
                snap["cursor"] = {{"rune", 1}, {"utf16", 1}};
            }
            send(fd, {{"ok", true}, {"snapshot", snap}});
        }
        close(fd);
    });
    {
        zuin::Client client(path, "/bin/false");
        zuin::Session session(client);
        const auto snap = session.key(japanese ? "Convert" : "a");
        EXPECT(snap.ok);
        EXPECT(session.japaneseCapable() == japanese);
        EXPECT(session.convertKeys() == japanese);
        EXPECT(session.japaneseKeys() == japanese);
        EXPECT(session.clauseDisplay() == japanese);
        EXPECT(session.japanese() == japanese);
        if (japanese) {
            EXPECT(snap.japanese && snap.language == "ja" && snap.inputMethod == "romaji");
            EXPECT(snap.converting && snap.clauseIndex == 1 && snap.clauseCount == 2);
            EXPECT(session.language() == "ja" && session.inputMethod() == "romaji");
            EXPECT(snap.preedit.size() == 2);
            EXPECT(snap.preedit[0].style == "clause" && snap.preedit[1].style == "clause_active");
            EXPECT(snap.text() == "今日は" && snap.candidates.size() == 1 && snap.candidates[0].second == "今日");
            EXPECT(snap.candidateKeys.size() == 1 && snap.candidateKeys[0] == "1");
        } else {
            // 舊引擎沒有日文欄位：外殼照舊走注音，不快取任何日文狀態。
            EXPECT(!snap.japanese && snap.language.empty() && !snap.converting);
            EXPECT(snap.clauseIndex == 0 && snap.clauseCount == 0);
            EXPECT(session.language().empty() && session.inputMethod().empty());
            EXPECT(snap.text() == "我");
        }
    }
    server.join();
    close(listener);
    unlink(path.c_str());
}

int main() {
    run(true);
    run(false);
    if (failures) { std::fprintf(stderr, "%d failure(s)\n", failures); return 1; }
    std::puts("japanese_test ok");
    return 0;
}
