import React, { createContext, useContext, useEffect, useState } from 'react';
import { Journey, JourneyStatus, Notification } from '../data/mockData';
import { loginApi, logoutApi, clearTokens, AuthUser } from '../services/auth';
import * as api from '../services/journeyApi';
import * as notifApi from '../services/notificationApi';

export type UserRole = 'driver' | 'admin';

export interface User {
  id: string;
  name: string;
  email: string;
  role: UserRole;
  phone?: string;
  vehicleId?: string;
  vehicle_type?: string;
  avatar?: string;
}

export interface BookingData {
  origin: string;
  destination: string;
  originCoords?: { lat: number; lng: number };
  destCoords?: { lat: number; lng: number };
  departureTime: string;
  vehicleType: string;
}

export interface BookingResult {
  success: boolean;
  journeyId: string;
  reason?: string;
  journey?: Journey;
}

interface AppContextType {
  user: User | null;
  isAuthenticated: boolean;
  login: (email: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  journeys: Journey[];
  adminJourneys: Journey[];
  notifications: Notification[];
  unreadCount: number;
  lastBookingResult: BookingResult | null;
  bookJourney: (data: BookingData) => Promise<BookingResult>;
  updateJourneyStatus: (id: string, status: JourneyStatus, by?: string) => Promise<void>;
  markNotificationRead: (id: string) => Promise<void>;
  markAllRead: () => Promise<void>;
  clearBookingResult: () => void;
}

const AppContext = createContext<AppContextType | null>(null);

function userFromAuth(auth: AuthUser): User {
  return {
    id: auth.id,
    name: auth.name,
    email: auth.email,
    role: auth.role,
    vehicle_type: auth.vehicle_type,
  };
}

function mapApiNotification(n: notifApi.ApiNotification): Notification {
  return {
    id: n.id,
    title: n.title,
    message: n.message,
    type: n.type,
    read: n.read,
    timestamp: n.timestamp,
    journeyId: n.journey_id || undefined,
  };
}

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(() => {
    try {
      const stored = localStorage.getItem('cw_user');
      return stored ? JSON.parse(stored) : null;
    } catch {
      return null;
    }
  });

  const [journeys, setJourneys] = useState<Journey[]>([]);
  const [adminJourneys, setAdminJourneys] = useState<Journey[]>([]);
  const [notifications, setNotifications] = useState<Notification[]>([]);
  const [lastBookingResult, setLastBookingResult] = useState<BookingResult | null>(null);

  const unreadCount = notifications.filter((n) => !n.read).length;

  // Load journeys and notifications when user logs in
  useEffect(() => {
    if (!user) return;
    const load = async () => {
      try {
        if (user.role === 'driver') {
          const { journeys: apiJourneys } = await api.listJourneys(user.id);
          setJourneys(apiJourneys);
        } else if (user.role === 'admin') {
          const { journeys: apiJourneys } = await api.adminListJourneys();
          setAdminJourneys(apiJourneys);
        }
      } catch {
        // API unavailable — leave empty
      }

      try {
        const res = await notifApi.listNotifications();
        setNotifications(res.notifications.map(mapApiNotification));
      } catch {
        // Notification service unavailable — leave empty
      }
    };
    load();
  }, [user?.id]);

  const login = async (email: string, password: string): Promise<void> => {
    const result = await loginApi(email, password);
    const u = userFromAuth(result.user);
    setUser(u);
    localStorage.setItem('cw_user', JSON.stringify(u));
  };

  const logout = async (): Promise<void> => {
    // Clear local state immediately so the UI responds instantly
    setUser(null);
    setJourneys([]);
    setAdminJourneys([]);
    setNotifications([]);
    localStorage.removeItem('cw_user');
    clearTokens();
    // Best-effort server-side logout (fire-and-forget)
    logoutApi().catch(() => {});
  };

  const bookJourney = async (data: BookingData): Promise<BookingResult> => {
    try {
      const journey = await api.createJourney(data);
      const result: BookingResult = {
        success: journey.status !== 'rejected',
        journeyId: journey.id,
        reason: journey.rejectionReason,
        journey,
      };
      setJourneys((prev) => [journey, ...prev]);
      setAdminJourneys((prev) => [journey, ...prev]);
      setLastBookingResult(result);

      // Refresh notifications after booking (backend may have sent one)
      setTimeout(async () => {
        try {
          const res = await notifApi.listNotifications();
          setNotifications(res.notifications.map(mapApiNotification));
        } catch { /* ignore */ }
      }, 2000);

      return result;
    } catch (err) {
      // Re-throw so the page can display the error
      throw err;
    }
  };

  const updateJourneyStatus = async (id: string, status: JourneyStatus): Promise<void> => {
    let updated: Journey | undefined;
    if (status === 'cancelled') updated = await api.cancelJourney(id);
    else if (status === 'active') updated = await api.activateJourney(id);
    else if (status === 'completed') updated = await api.completeJourney(id);

    if (updated) {
      setJourneys((prev) =>
        prev.map((j) => (j.id === id ? { ...updated!, segments: j.segments, timeline: j.timeline } : j)),
      );
      setAdminJourneys((prev) =>
        prev.map((j) => (j.id === id ? { ...updated!, segments: j.segments, timeline: j.timeline } : j)),
      );
    }

    // Refresh notifications
    try {
      const res = await notifApi.listNotifications();
      setNotifications(res.notifications.map(mapApiNotification));
    } catch { /* ignore */ }
  };

  const markNotificationRead = async (id: string): Promise<void> => {
    // Optimistic update
    setNotifications((prev) => prev.map((n) => (n.id === id ? { ...n, read: true } : n)));
    try {
      await notifApi.markNotificationRead(id);
    } catch { /* ignore — optimistic update already applied */ }
  };

  const markAllRead = async (): Promise<void> => {
    // Optimistic update
    setNotifications((prev) => prev.map((n) => ({ ...n, read: true })));
    try {
      await notifApi.markAllNotificationsRead();
    } catch { /* ignore */ }
  };

  const clearBookingResult = () => setLastBookingResult(null);

  return (
    <AppContext.Provider
      value={{
        user,
        isAuthenticated: !!user,
        login,
        logout,
        journeys,
        adminJourneys,
        notifications,
        unreadCount,
        lastBookingResult,
        bookJourney,
        updateJourneyStatus,
        markNotificationRead,
        markAllRead,
        clearBookingResult,
      }}
    >
      {children}
    </AppContext.Provider>
  );
}

export const useApp = () => {
  const ctx = useContext(AppContext);
  if (!ctx) throw new Error('useApp must be used within AppProvider');
  return ctx;
};
