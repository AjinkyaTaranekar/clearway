import { getApp, getApps, initializeApp } from 'firebase/app';
import { getMessaging, isSupported, type Messaging } from 'firebase/messaging';

const firebaseConfig = {
  apiKey: 'AIzaSyB6QUKp9g3W3jvQ1W6_sPjLkqkGc1QOXus',
  authDomain: 'distributed-capacity-system.firebaseapp.com',
  projectId: 'distributed-capacity-system',
  storageBucket: 'distributed-capacity-system.firebasestorage.app',
  messagingSenderId: '197986306314',
  appId: '1:197986306314:web:b3c501294b73371cc592f3',
};

export const firebaseApp = getApps().length ? getApp() : initializeApp(firebaseConfig);

let messagingPromise: Promise<Messaging | null> | null = null;

export function getFirebaseMessaging(): Promise<Messaging | null> {
  if (!messagingPromise) {
    messagingPromise = isSupported()
      .then((supported) => (supported ? getMessaging(firebaseApp) : null))
      .catch(() => null);
  }
  return messagingPromise;
}