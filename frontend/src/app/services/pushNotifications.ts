import { deleteToken, getMessaging, getToken } from 'firebase/messaging';
import { firebaseApp } from './firebase';
import { deactivateDeviceToken, registerDeviceToken } from './notificationApi';

const PUSH_ENABLED_KEY = 'cw_push_enabled';
const PUSH_TOKEN_KEY = 'cw_push_token';
const PUSH_PROMPT_DISMISSED_KEY = 'cw_push_prompt_dismissed';
const FCM_SW_PATH = '/firebase-messaging-sw.js';
const PUSH_LOG_PREFIX = '[Push]';

function logInfo(message: string, value?: unknown): void {
  if (value === undefined) {
    console.info(`${PUSH_LOG_PREFIX} ${message}`);
    return;
  }
  console.info(`${PUSH_LOG_PREFIX} ${message}`, value);
}

function logWarn(message: string, value?: unknown): void {
  if (value === undefined) {
    console.warn(`${PUSH_LOG_PREFIX} ${message}`);
    return;
  }
  console.warn(`${PUSH_LOG_PREFIX} ${message}`, value);
}

function logError(message: string, value?: unknown): void {
  if (value === undefined) {
    console.error(`${PUSH_LOG_PREFIX} ${message}`);
    return;
  }
  console.error(`${PUSH_LOG_PREFIX} ${message}`, value);
}

function ensureBrowserPushSupport(): void {
  if (typeof window === 'undefined' || !('Notification' in window)) {
    throw new Error('This browser does not support notifications.');
  }
  if (!('serviceWorker' in navigator)) {
    throw new Error('This browser does not support service workers required for push notifications.');
  }
}

function getRequiredVapidKey(): string {
  const vapidKey = import.meta.env.VITE_FIREBASE_VAPID_KEY;
  if (!vapidKey) {
    throw new Error('Missing VITE_FIREBASE_VAPID_KEY for Firebase web push.');
  }
  return vapidKey;
}

async function requestPermission(): Promise<void> {
  logInfo('Requesting notification permission...');
  const permission = await Notification.requestPermission();
  logInfo(`Notification permission result: ${permission}`);

  if (permission !== 'granted') {
    throw new Error('Notification permission was not granted.');
  }
}

async function getFirebaseWebToken(): Promise<string> {
  ensureBrowserPushSupport();
  const vapidKey = getRequiredVapidKey();

  const registration = await navigator.serviceWorker.register(FCM_SW_PATH, { scope: '/' });
  const messaging = getMessaging(firebaseApp);

  logInfo('Getting Firebase registration token...');
  const token = await getToken(messaging, { vapidKey, serviceWorkerRegistration: registration });
  if (!token) {
    throw new Error('No registration token available. Request permission to generate one.');
  }

  logInfo('Firebase registration token acquired:', token);
  return token;
}

async function getConfiguredToken(): Promise<string> {
  const stored = localStorage.getItem(PUSH_TOKEN_KEY);
  if (stored) {
    logInfo('Using cached push token from local storage.');
    return stored;
  }

  const token = await getFirebaseWebToken();
  localStorage.setItem(PUSH_TOKEN_KEY, token);
  return token;
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

  try {
    const messaging = getMessaging(firebaseApp);
    await deleteToken(messaging);
    logInfo('Firebase token deleted from browser cache.');
  } catch (err) {
    logWarn('Failed to delete Firebase token from browser cache (best effort).', err);
  }
}

function showLocalTestNotification(): void {
  if (typeof window === 'undefined' || !('Notification' in window)) {
    return;
  }
  if (Notification.permission !== 'granted') {
    return;
  }

  try {
    new Notification('Clearway test notification', {
      body: 'Push notifications are enabled on this browser.',
      tag: 'clearway-push-test',
    });
    logInfo('Displayed local test notification.');
  } catch (err) {
    logWarn('Could not show local test notification.', err);
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
  ensureBrowserPushSupport();

  if (Notification.permission === 'denied') {
    throw new Error('Notifications are blocked in this browser. Please re-enable them in site settings.');
  }

  if (Notification.permission !== 'granted') {
    await requestPermission();
  }

  const token = await getConfiguredToken();
  logInfo('Sending device token to backend...');
  await registerDeviceToken(token, 'web');
  logInfo('Device token successfully registered with backend.');

  localStorage.setItem(PUSH_ENABLED_KEY, 'true');
  localStorage.removeItem(PUSH_PROMPT_DISMISSED_KEY);

  // Quick browser-side smoke test after successful registration.
  showLocalTestNotification();
}

export async function disablePushNotifications(): Promise<void> {
  const token = localStorage.getItem(PUSH_TOKEN_KEY);
  if (token) {
    logInfo('Deactivating push token in backend...');
    await deactivateDeviceToken(token);
    logInfo('Push token deactivated in backend.');
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

  try {
    const token = await tryGetConfiguredToken();
    if (!token) return;

    logInfo('Syncing push token with backend after auth refresh/login...');
    await registerDeviceToken(token, 'web');
    localStorage.setItem(PUSH_ENABLED_KEY, 'true');
    logInfo('Push token sync completed.');
  } catch (err) {
    logError('Push token sync failed.', err);
    throw err;
  }
}
