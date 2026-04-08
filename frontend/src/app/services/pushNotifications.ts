import { registerDeviceToken } from './notificationApi';

const PUSH_ENABLED_KEY = 'cw_push_enabled';
const PUSH_TOKEN_KEY = 'cw_push_token';

function getConfiguredToken(): string {
  const stored = localStorage.getItem(PUSH_TOKEN_KEY);
  if (stored) return stored;

  const configured = import.meta.env.VITE_FCM_WEB_TOKEN;
  if (!configured) {
    throw new Error('Push is not configured for this frontend yet. Set VITE_FCM_WEB_TOKEN to a valid browser FCM token.');
  }

  localStorage.setItem(PUSH_TOKEN_KEY, configured);
  return configured;
}

export function isPushEnabled(): boolean {
  return localStorage.getItem(PUSH_ENABLED_KEY) === 'true';
}

export async function enablePushNotifications(): Promise<void> {
  if (typeof window === 'undefined' || !('Notification' in window)) {
    throw new Error('This browser does not support notifications.');
  }

  if (Notification.permission === 'denied') {
    throw new Error('Notifications are blocked in this browser. Please re-enable them in site settings.');
  }

  if (Notification.permission !== 'granted') {
    const permission = await Notification.requestPermission();
    if (permission !== 'granted') {
      throw new Error('Notification permission was not granted.');
    }
  }

  const token = getConfiguredToken();
  await registerDeviceToken(token, 'web');
  localStorage.setItem(PUSH_ENABLED_KEY, 'true');
}

export function disablePushNotifications(): void {
  localStorage.setItem(PUSH_ENABLED_KEY, 'false');
}
