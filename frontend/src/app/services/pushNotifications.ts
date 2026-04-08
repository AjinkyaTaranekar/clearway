import { deleteToken, getToken } from 'firebase/messaging';
import { getFirebaseMessaging } from './firebase';
import { deactivateDeviceToken, registerDeviceToken } from './notificationApi';

const PUSH_ENABLED_KEY = 'cw_push_enabled';
const PUSH_TOKEN_KEY = 'cw_push_token';
const FCM_SW_PATH = '/firebase-messaging-sw.js';

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

  const serviceWorkerRegistration = await navigator.serviceWorker.register(FCM_SW_PATH);
  const token = await getToken(messaging, { vapidKey, serviceWorkerRegistration });
  return token || null;
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
}

export async function disablePushNotifications(): Promise<void> {
  const token = await tryGetConfiguredToken();
  if (token) {
    await deactivateDeviceToken(token);
  }
  await revokeFirebaseTokenIfAvailable();
  localStorage.removeItem(PUSH_TOKEN_KEY);
  localStorage.setItem(PUSH_ENABLED_KEY, 'false');
}

// Re-registers the current browser token after authentication refresh/login
// when push was previously enabled in this browser.
export async function syncPushRegistrationIfEnabled(): Promise<void> {
  if (!isPushEnabled()) return;
  const token = await tryGetConfiguredToken();
  if (!token) return;
  await registerDeviceToken(token, 'web');
}
