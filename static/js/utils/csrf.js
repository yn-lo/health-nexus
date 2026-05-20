/**
 * CSRF工具函数
 * 从meta标签读取CSRF Token
 */
function getCSRFToken() {
    var meta = document.querySelector('meta[name="csrf-token"]');
    if (!meta) {
        throw new Error('CSRF token meta tag not found. Please ensure <meta name="csrf-token"> exists in the page head.');
    }
    return meta.getAttribute('content');
}
