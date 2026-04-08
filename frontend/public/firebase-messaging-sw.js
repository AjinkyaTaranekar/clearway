importScripts('https://www.gstatic.com/firebasejs/12.11.0/firebase-app-compat.js');
importScripts('https://www.gstatic.com/firebasejs/12.11.0/firebase-messaging-compat.js');

firebase.initializeApp({
  apiKey: 'AIzaSyB6QUKp9g3W3jvQ1W6_sPjLkqkGc1QOXus',
  authDomain: 'distributed-capacity-system.firebaseapp.com',
  projectId: 'distributed-capacity-system',
  storageBucket: 'distributed-capacity-system.firebasestorage.app',
  messagingSenderId: '197986306314',
  appId: '1:197986306314:web:b3c501294b73371cc592f3',
});

const messaging = firebase.messaging();

messaging.onBackgroundMessage((payload) => {
  const notificationTitle = payload.notification?.title || 'Clearway notification';
  const notificationOptions = {
    body: payload.notification?.body || 'You have a new update.',
    data: payload.data || {},
  };

  self.registration.showNotification(notificationTitle, notificationOptions);
});
