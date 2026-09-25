// 알림 벨 UI: script.js는 건드리지 않고 별도 파일로 분리.
// 로그인 상태는 script.js가 사용자 정보를 다 불러온 뒤 호출해주는
// window.onUserStateReady(user) 훅으로 전달받는다.
(function () {
	"use strict";

	var TYPE_LABEL = {
		like_on_comment: "님이 내 댓글을 좋아합니다.",
		like_on_fanart: "님이 내 팬아트를 좋아합니다.",
		like_on_post: "님이 내 게시물을 좋아합니다.",
		comment_on_post: "님이 내 게시물에 댓글을 남겼습니다.",
		comment_on_fanart: "님이 내 팬아트에 댓글을 남겼습니다."
	};

	var notifWrap, notifBellBtn, notifBadge, notifDropdown, notifList, notifMarkAllBtn;
	var eventSource = null;
	var isOpen = false;
	var isLoggedIn = false;

	function el(id) {
		return document.getElementById(id);
	}

	function safeEscape(s) {
		if (typeof window.escapeHtml === "function") return window.escapeHtml(s == null ? "" : String(s));
		var d = document.createElement("div");
		d.textContent = s == null ? "" : String(s);
		return d.innerHTML;
	}

	function updateBadge(count) {
		if (!notifBadge) return;
		if (count > 0) {
			notifBadge.textContent = count > 99 ? "99+" : String(count);
			notifBadge.style.display = "flex";
		} else {
			notifBadge.style.display = "none";
		}
	}

	function renderList(notifications) {
		if (!notifList) return;
		if (!notifications || notifications.length === 0) {
			notifList.innerHTML = '<div class="pl-notif-empty">아직 알림이 없어요.</div>';
			return;
		}
		notifList.innerHTML = notifications
			.map(function (n) {
				var label = TYPE_LABEL[n.type] || "새 알림이 있습니다.";
				var preview = n.preview_text ? '<span class="pl-notif-preview">' + safeEscape(n.preview_text) + "</span>" : "";
				return (
					'<button class="pl-notif-item ' + (n.is_read ? "" : "unread") + '" data-id="' + n.id + '">' +
					"<strong>" + safeEscape(n.actor_nickname || "누군가") + "</strong>" + safeEscape(label) +
					preview +
					'<span class="pl-notif-date">' + safeEscape(n.date || "") + "</span>" +
					"</button>"
				);
			})
			.join("");
	}

	function loadNotifications() {
		fetch("/api/notifications", { cache: "no-store" })
			.then(function (r) { return r.json(); })
			.then(function (data) {
				if (!data || !data.success) return;
				updateBadge(data.unread_count || 0);
				renderList(data.notifications || []);
			})
			.catch(function () {});
	}

	function markAllRead() {
		fetch("/api/notifications/read", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({})
		})
			.then(function () {
				updateBadge(0);
				loadNotifications();
			})
			.catch(function () {});
	}

	function markOneRead(id) {
		fetch("/api/notifications/read", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify({ id: Number(id) })
		})
			.then(loadNotifications)
			.catch(function () {});
	}

	function toggleDropdown(forceOpen) {
		isOpen = typeof forceOpen === "boolean" ? forceOpen : !isOpen;
		if (notifDropdown) notifDropdown.classList.toggle("open", isOpen);
		if (isOpen) loadNotifications();
	}

	function startStream() {
		if (eventSource || !isLoggedIn) return;
		if (typeof window.EventSource !== "function") return;
		try {
			eventSource = new EventSource("/api/notifications/stream");
			eventSource.addEventListener("notification", function (ev) {
				var payload = {};
				try { payload = JSON.parse(ev.data); } catch (e) {}
				updateBadge(payload.unread_count || 0);
				if (isOpen) loadNotifications();
				if (typeof window.showToast === "function") {
					var label = TYPE_LABEL[payload.type] || "새 알림이 도착했어요.";
					window.showToast((payload.actor_nickname || "누군가") + label);
				}
			});
			eventSource.onerror = function () {
				// 연결이 끊기면 브라우저가 자동 재연결을 시도한다. 계속 실패하면
				// (예: 로그아웃) stopStream()이 정리해줄 것.
			};
		} catch (e) {}
	}

	function stopStream() {
		if (eventSource) {
			eventSource.close();
			eventSource = null;
		}
	}

	function setLoggedIn(loggedIn) {
		isLoggedIn = !!loggedIn;
		if (notifWrap) notifWrap.style.display = isLoggedIn ? "" : "none";
		if (isLoggedIn) {
			loadNotifications();
			startStream();
		} else {
			stopStream();
			updateBadge(0);
			if (notifList) notifList.innerHTML = "";
			toggleDropdown(false);
		}
	}

	function init() {
		notifWrap = el("notifWrap");
		notifBellBtn = el("notifBellBtn");
		notifBadge = el("notifBadge");
		notifDropdown = el("notifDropdown");
		notifList = el("notifList");
		notifMarkAllBtn = el("notifMarkAllBtn");
		if (!notifWrap || !notifBellBtn) return;

		notifBellBtn.addEventListener("click", function (e) {
			e.stopPropagation();
			toggleDropdown();
		});
		document.addEventListener("click", function () {
			if (isOpen) toggleDropdown(false);
		});
		if (notifDropdown) {
			notifDropdown.addEventListener("click", function (e) { e.stopPropagation(); });
		}
		if (notifMarkAllBtn) {
			notifMarkAllBtn.addEventListener("click", markAllRead);
		}
		if (notifList) {
			notifList.addEventListener("click", function (e) {
				var item = e.target.closest(".pl-notif-item");
				if (!item) return;
				markOneRead(item.getAttribute("data-id"));
			});
		}

		// script.js가 로그인 상태를 확인한 뒤 호출해주는 훅. script.js 로드 순서와
		// 무관하게 안전하게 이어붙인다(이미 등록된 콜백이 있으면 체이닝).
		var prev = window.onUserStateReady;
		window.onUserStateReady = function (user) {
			if (typeof prev === "function") {
				try { prev(user); } catch (e) {}
			}
			setLoggedIn(!!(user && user.logged_in !== false));
		};
	}

	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", init);
	} else {
		init();
	}
})();
