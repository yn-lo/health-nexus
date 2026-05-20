/**
 * API工具函数
 * 统一的API请求封装，自动处理CSRF Token
 */

/**
 * 发送POST请求
 * @param {string} url - 请求URL
 * @param {Object} body - 请求体对象，将被序列化为JSON
 * @param {Object} options - 可选配置 { headers?: Object }
 * @returns {Promise<{ ok: boolean, data?: Object, error?: string, status?: number }>}
 */
function apiPost(url, body, options) {
    options = options || {};
    
    var headers = options.headers || {};
    headers['Content-Type'] = 'application/json';
    
    try {
        headers['X-CSRFToken'] = getCSRFToken();
    } catch (e) {
        return Promise.reject(new Error('Failed to get CSRF token: ' + e.message));
    }
    
    return fetch(url, {
        method: 'POST',
        headers: headers,
        body: JSON.stringify(body),
        credentials: 'same-origin'
    })
    .then(function(response) {
        var status = response.status;
        return response.json()
            .then(function(data) {
                if (response.ok) {
                    return { ok: true, data: data, status: status };
                } else {
                    var errorMessage = data.message || data.error || 'Request failed with status ' + status;
                    return { ok: false, error: errorMessage, status: status };
                }
            })
            .catch(function() {
                if (response.ok) {
                    return { ok: true, data: null, status: status };
                } else {
                    return { ok: false, error: 'Request failed with status ' + status, status: status };
                }
            });
    })
    .catch(function(error) {
        return { ok: false, error: 'Network error: ' + error.message };
    });
}

/**
 * 发送POST请求，期望返回HTML
 * @param {string} url - 请求URL
 * @param {Object} body - 请求体对象，将被序列化为表单格式
 * @param {Object} options - 可选配置 { headers?: Object }
 * @returns {Promise<{ ok: boolean, html?: string, error?: Error, status?: number }>}
 */
function apiPostHtml(url, body, options) {
    options = options || {};

    var headers = options.headers || {};
    headers['Content-Type'] = 'application/x-www-form-urlencoded';

    try {
        headers['X-CSRFToken'] = getCSRFToken();
    } catch (e) {
        return Promise.reject(new Error('Failed to get CSRF token: ' + e.message));
    }

    var bodyStr = '';
    if (body) {
        var parts = [];
        for (var key in body) {
            if (body.hasOwnProperty(key)) {
                parts.push(encodeURIComponent(key) + '=' + encodeURIComponent(body[key]));
            }
        }
        bodyStr = parts.join('&');
    }

    return fetch(url, {
        method: 'POST',
        headers: headers,
        body: bodyStr || undefined,
        credentials: 'same-origin'
    })
    .then(function(response) {
        var status = response.status;
        return response.text()
            .then(function(html) {
                if (response.ok) {
                    return { ok: true, html: html, status: status };
                } else {
                    return { ok: false, error: new Error('Request failed with status ' + status), status: status };
                }
            });
    })
    .catch(function(error) {
        return { ok: false, error: error, status: 0 };
    });
}
