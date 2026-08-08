// 這個頁面只需要兩件事：定時把某個區塊換成 server 重新渲染的 HTML，以及按鈕
// 發一個請求後刷新。刻意不引入前端框架 —— 那需要 vendoring 一份 min.js，而規範
// 禁止 CDN 依賴（這個服務常常跑在沒有對外網路的機器上）。

'use strict';

// startPolling 讓每個帶 data-poll-url 的容器定時抓 fragment 換掉自己的內容。
//
// 抓失敗時刻意不清空既有內容：把上一次成功取得的畫面留著，遠好過因為一次網路
// 抖動就讓使用者看到一片空白。
function startPolling() {
    document.querySelectorAll('[data-poll-url]').forEach(function (container) {
        const url = container.dataset.pollUrl;
        const interval = parseInt(container.dataset.pollInterval, 10) || 15000;

        setInterval(function () {
            // 分頁在背景時不打 server：沒人在看的畫面不需要更新。
            if (document.hidden) {
                return;
            }

            fetch(url, { headers: { 'Accept': 'text/html' } })
                .then(function (response) {
                    if (!response.ok) {
                        throw new Error('HTTP ' + response.status);
                    }
                    return response.text();
                })
                .then(function (html) {
                    container.innerHTML = html;
                })
                .catch(function () {
                    // 保留上一次的內容，安靜地等下一輪。
                });
        }, interval);
    });
}

// postAction 發一個改變狀態的請求，然後重新載入頁面。
//
// 直接 reload 而不做局部更新：這些動作會改變好幾處畫面（狀態燈、下次執行時間、
// 按鈕本身），整頁重載是最不會出錯的做法，而這是一個個人用的本機工具。
function postAction(url, method) {
    const button = window.event && window.event.target;
    if (button && button.tagName === 'BUTTON') {
        button.disabled = true;
    }

    fetch(url, { method: method, headers: { 'Accept': 'application/json' } })
        .then(function (response) {
            return response.json().catch(function () {
                return {};
            }).then(function (body) {
                return { ok: response.ok, status: response.status, body: body };
            });
        })
        .then(function (result) {
            if (!result.ok) {
                showToast(describeError(result), 'error');
                if (button && button.tagName === 'BUTTON') {
                    button.disabled = false;
                }
                return;
            }

            if (result.body && result.body.runStatus) {
                showToast('執行結束：' + result.body.runStatus, result.body.runStatus === 'succeeded' ? 'success' : 'error');
                setTimeout(function () { window.location.reload(); }, 1200);
                return;
            }

            window.location.reload();
        })
        .catch(function (error) {
            showToast('請求失敗：' + error.message, 'error');
            if (button && button.tagName === 'BUTTON') {
                button.disabled = false;
            }
        });
}

function submitCreateForm(event) {
    event.preventDefault();

    const form = event.target;
    const payload = {
        scheduleExpression: form.scheduleExpression.value,
        command: form.command.value,
        description: form.description.value
    };

    fetch('/jobs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'Accept': 'application/json' },
        body: JSON.stringify(payload)
    })
        .then(function (response) {
            return response.json().catch(function () {
                return {};
            }).then(function (body) {
                return { ok: response.ok, status: response.status, body: body };
            });
        })
        .then(function (result) {
            if (!result.ok) {
                showToast(describeError(result), 'error');
                return;
            }
            window.location.reload();
        })
        .catch(function (error) {
            showToast('請求失敗：' + error.message, 'error');
        });

    return false;
}

// adoptJob 在動手之前先講清楚後果：輸出位置會換地方。
function adoptJob(jobId, currentLogFilePath) {
    let message = '轉為納管後，這個 job 的執行會經過 crontab-watcher 的 wrapper，' +
        '因此會有完整的 exit code 與逐次執行紀錄。\n\n';

    if (currentLogFilePath) {
        message += '注意：指令尾端的 redirect 會被移除，輸出從此寫進 crontab-watcher 管理的 log 檔，' +
            '不再寫入 ' + currentLogFilePath + '。\n\n（被移除的 redirect 會記在 crontab 的註解裡以便還原。）';
    }

    if (!window.confirm(message)) {
        return;
    }

    postAction('/jobs/' + jobId + '/adopt', 'POST');
}

function deleteJob(jobId) {
    if (!window.confirm('確定要從 crontab 刪除這個 job？\n\n刪除是移除該行；若只是想暫時停掉，用「停用」會保留原文、之後可以完整還原。')) {
        return;
    }

    fetch('/jobs/' + jobId, { method: 'DELETE', headers: { 'Accept': 'application/json' } })
        .then(function (response) {
            if (!response.ok) {
                return response.json().catch(function () {
                    return {};
                }).then(function (body) {
                    showToast(describeError({ status: response.status, body: body }), 'error');
                });
            }
            window.location.href = '/';
        })
        .catch(function (error) {
            showToast('請求失敗：' + error.message, 'error');
        });
}

function describeError(result) {
    const body = result.body || {};
    let message = body.error || ('HTTP ' + result.status);

    if (body.hint) {
        message += '\n' + body.hint;
    }

    return message;
}

function showToast(message, kind) {
    const existing = document.querySelector('.toast');
    if (existing) {
        existing.remove();
    }

    const toast = document.createElement('div');
    toast.className = 'toast toast-' + (kind || 'info');
    toast.textContent = message;
    document.body.appendChild(toast);

    setTimeout(function () {
        toast.remove();
    }, 8000);
}

startPolling();
