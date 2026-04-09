import { deleteToken, getToken } from 'firebase/messaging';
import { getFirebaseMessaging } from './firebase';
import { deactivateDeviceToken, registerDeviceToken } from './notificationApi';

const PUSH_ENABLED_KEY = 'cw_push_enabled';
const PUSH_TOKEN_KEY = 'cw_push_token';
const PUSH_PROMPT_DISMISSED_KEY = 'cw_push_prompt_dismissed';
const FCM_SW_PATH = '/firebase-messaging-sw.js';

async function waitForActiveServiceWorker(registration: ServiceWorkerRegistration): Promise<ServiceWorkerRegistration> {
  if (registration.active) {
    return registration;
  }

  const pendingWorker = registration.installing ?? registration.waiting;
  if (pendingWorker) {
    await new Promise<void>((resolve) => {
      const onStateChange = () => {
        if (pendingWorker.state === 'activated') {
          pendingWorker.removeEventListener('statechange', onStateChange);
          resolve();
        }
      };

      pendingWorker.addEventListener('statechange', onStateChange);
      setTimeout(() => {
        pendingWorker.removeEventListener('statechange', onStateChange);
        resolve();
      }, 8000);
    });
  }

  if (registration.active) {
    return registration;
  }

  return navigator.serviceWorker.ready;
}

function getLegacyToken(): string | null {
  return import.meta.env.VITE_FCM_WEB_TOKEN || null;
}

async function getFirebaseWebToken(): Promise<string | null> {
  if (typeof window === 'undefined' || !('serviceWorker' in navigator)) {
    return null;
  }

  const vapidKey = import.meta.env.VITE_FIREBASE_VAPID_KEY;
  if (!vapidKey) {
    return null;
  }

  const messaging = await getFirebaseMessaging();
  if (!messaging) {
    return null;
  }

  const serviceWorkerRegistration = await navigator.serviceWorker.register(FCM_SW_PATH, { scope: '/' });
  const activeRegistration = await waitForActiveServiceWorker(serviceWorkerRegistration);

  try {
    const token = await getToken(messaging, { vapidKey, serviceWorkerRegistration: activeRegistration });
    return token || null;
  } catch (err) {
    const message = err instanceof Error ? err.message.toLowerCase() : '';
    if (!message.includes('no active service worker')) {
      throw err;
    }

    // Best-effort retry after the browser reports the registration as ready.
    const readyRegistration = await navigator.serviceWorker.ready;
    const retryToken = await getToken(messaging, { vapidKey, serviceWorkerRegistration: readyRegistration });
    return retryToken || null;
  }
}

async function getConfiguredToken(): Promise<string> {
  const stored = localStorage.getItem(PUSH_TOKEN_KEY);
  if (stored) return stored;

  const firebaseToken = await getFirebaseWebToken();
  if (firebaseToken) {
    localStorage.setItem(PUSH_TOKEN_KEY, firebaseToken);
    return firebaseToken;
  }

  const configured = getLegacyToken();
  if (!configured) {
    throw new Error('Push is not configured yet. Set VITE_FIREBASE_VAPID_KEY for Firebase Messaging or provide legacy VITE_FCM_WEB_TOKEN.');
  }

  localStorage.setItem(PUSH_TOKEN_KEY, configured);
  return configured;
}

async function tryGetConfiguredToken(): Promise<string | null> {
  try {
    return await getConfiguredToken();
  } catch {
    return null;
  }
}

async function revokeFirebaseTokenIfAvailable(): Promise<void> {
  if (typeof window === 'undefined' || !('serviceWorker' in navigator)) {
    return;
  }

  if (!import.meta.env.VITE_FIREBASE_VAPID_KEY) {
    return;
  }

  const messaging = await getFirebaseMessaging();
  if (!messaging) {
    return;
  }

  try {
    await deleteToken(messaging);
  } catch {
    // best-effort cleanup only
  }
}

export function isPushEnabled(): boolean {
  return localStorage.getItem(PUSH_ENABLED_KEY) === 'true';
}

export function shouldShowPushPermissionPrompt(): boolean {
  if (typeof window === 'undefined' || !('Notification' in window)) {
    return false;
  }

  if (Notification.permission !== 'default') {
    return false;
  }

  // Respect explicit opt-out preference.
  if (localStorage.getItem(PUSH_ENABLED_KEY) === 'false') {
    return false;
  }

  return localStorage.getItem(PUSH_PROMPT_DISMISSED_KEY) !== 'true';
}

export function dismissPushPermissionPrompt(): void {
  localStorage.setItem(PUSH_PROMPT_DISMISSED_KEY, 'true');
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

  const token = await getConfiguredToken();
  await registerDeviceToken(token, 'web');
  localStorage.setItem(PUSH_ENABLED_KEY, 'true');
  localStorage.removeItem(PUSH_PROMPT_DISMISSED_KEY);
}

export async function disablePushNotifications(): Promise<void> {
  const token = await tryGetConfiguredToken();
  if (token) {
    await deactivateDeviceToken(token);
  }
  await revokeFirebaseTokenIfAvailable();
  localStorage.removeItem(PUSH_TOKEN_KEY);
  localStorage.setItem(PUSH_ENABLED_KEY, 'false');
  localStorage.setItem(PUSH_PROMPT_DISMISSED_KEY, 'true');
}

// Re-registers the current browser token after authentication refresh/login
// when push was previously enabled in this browser.
export async function syncPushRegistrationIfEnabled(): Promise<void> {
  if (typeof window === 'undefined' || !('Notification' in window)) return;

  const storedPreference = localStorage.getItem(PUSH_ENABLED_KEY);
  // Honor explicit opt-out in settings.
  if (storedPreference === 'false') return;

  // If there is no saved preference but the browser permission is already
  // granted (e.g. cleared local storage), recover token registration.
  if (storedPreference !== 'true' && Notification.permission !== 'granted') return;

  const token = await tryGetConfiguredToken();
  if (!token) return;
  await registerDeviceToken(token, 'web');
  localStorage.setItem(PUSH_ENABLED_KEY, 'true');
}
