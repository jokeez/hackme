# Include inside server{} that proxies /api or /pool/coordinator/api.
error_page 429 = @hackme_json_429;
location @hackme_json_429 {
    default_type application/json;
    add_header Content-Type "application/json; charset=utf-8" always;
    return 429 '{"ok":false,"code":"rate_limited","error":"too many requests"}';
}
