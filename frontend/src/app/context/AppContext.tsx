import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { toast } from 'sonner';
import { Journey, JourneyStatus, Notification } from '../types';
import { clearTokens, getRefreshToken, getToken, storeTokens } from '../services/auth';
import { iamLogin, iamLogout, iamRegister, iamUpdateProfile, RegisterParams } from '../services/iamApi';
import * as api from '../services/journeyApi';
import * as notifApi from '../services/notificationApi';
import { syncPushRegistrationIfEnabled } from '../services/pushNotifications';

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
  priorityLevel?: 'normal' | 'max';
}

export interface BookingResult {
  success: boolean;
  journeyId: string;
  reason?: string;
  journey?: Journey;
}

export interface RegisterData {
  name: string;
  email: string;
  password: string;
  vehicleType: string;
  licenseNumber: string;
}

interface AppContextType {
  user: User | null;
  isAuthenticated: boolean;
  login: (email: string, password: string) => Promise<UserRole>;
  register: (data: RegisterData) => Promise<UserRole>;
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
  updateProfile: (fields: { name?: string; vehicle_type?: string }) => Promise<void>;
}

const AppContext = createContext<AppContextType | null>(null);

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
      if (!getToken()) {
        localStorage.removeItem('cw_user');
        return null;
      }
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

  const unreadCount = useMemo(() => notifications.filter((n) => !n.read).length, [notifications]);

  useEffect(() => {
    if (!user) return;
    if (getToken()) return;

    setUser(null);
    setJourneys([]);
    setAdminJourneys([]);
    setNotifications([]);
    setLastBookingResult(null);
    localStorage.removeItem('cw_user');
  }, [user]);

  useEffect(() => {
    if (!user) return;
    const load = async () => {
      try {
        if (user.role === 'driver') {
          const { journeys: apiJourneys } = await api.listJourneys(user.id);
          setJourneys(apiJourneys);
        } else {
          const { journeys: apiJourneys } = await api.adminListJourneys();
          setAdminJourneys(apiJourneys);
        }
      } catch (err) {
        toast.error('Failed to load journeys', {
          description: err instanceof Error ? err.message : 'Check your connection and try again.',
        });
      }

      try {
        const res = await notifApi.listNotifications();
        setNotifications(res.notifications.map(mapApiNotification));
      } catch {
        // notifications are non-critical — no toast
      }
    };
    load();
  }, [user?.id, user?.role]);

  // -------------------------------------------------------------------------
  // Auth
  // -------------------------------------------------------------------------

  const login = async (email: string, password: string): Promise<UserRole> => {
    const tokens = await iamLogin(email, password);
    storeTokens(tokens.access_token, tokens.refresh_token);
    const u: User = {
      id: tokens.user.id,
      name: tokens.user.name,
      email: tokens.user.email,
      role: tokens.user.role as UserRole,
      vehicle_type: tokens.user.vehicle_type,
    };
    setUser(u);
    localStorage.setItem('cw_user', JSON.stringify(u));
    void syncPushRegistrationIfEnabled().catch(() => undefined);
    return u.role;
  };

  const register = async (data: RegisterData): Promise<UserRole> => {
    const params: RegisterParams = {
      name: data.name,
      email: data.email,
      password: data.password,
      vehicle_type: data.vehicleType,
      license_info: { license_number: data.licenseNumber },
    };
    const tokens = await iamRegister(params);
    storeTokens(tokens.access_token, tokens.refresh_token);
    const u: User = {
      id: tokens.user.id,
      name: tokens.user.name,
      email: tokens.user.email,
      role: tokens.user.role as UserRole,
      vehicle_type: tokens.user.vehicle_type,
    };
    setUser(u);
    localStorage.setItem('cw_user', JSON.stringify(u));
    void syncPushRegistrationIfEnabled().catch(() => undefined);
    return u.role;
  };

  const logout = async (): Promise<void> => {
    const accessToken = getToken();
    const refreshToken = getRefreshToken();

    setUser(null);
    setJourneys([]);
    setAdminJourneys([]);
    setNotifications([]);
    setLastBookingResult(null);
    localStorage.removeItem('cw_user');
    clearTokens();

    if (accessToken && refreshToken) {
      try { await iamLogout(accessToken, refreshToken); } catch { /* best-effort */ }
    }
  };

  // -------------------------------------------------------------------------
  // Journeys
  // -------------------------------------------------------------------------

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

      setTimeout(async () => {
        try {
          const res = await notifApi.listNotifications();
          setNotifications(res.notifications.map(mapApiNotification));
        } catch { /* ignore */ }
      }, 1500);

      return result;
    } catch (err) {
      const reason = err instanceof Error ? err.message : 'Booking request failed. Please try again.';
      toast.error('Booking failed', { description: reason });
      const result: BookingResult = { success: false, journeyId: '', reason };
      setLastBookingResult(result);
      return result;
    }
  };

  const addStatusNotification = (id: string, status: JourneyStatus) => {
    const j = journeys.find((j) => j.id === id) ?? adminJourneys.find((j) => j.id === id);
    if (!j) return;
    const notif: Notification = {
      id: `N${Date.now()}`,
      title:
        status === 'active' ? 'Journey started'
        : status === 'completed' ? 'Journey completed'
        : 'Journey cancelled',
      message:
        status === 'active' ? `Your journey from ${j.origin} to ${j.destination} is now active. Drive safely.`
        : status === 'completed' ? `Your journey from ${j.origin} to ${j.destination} has been completed.`
        : `Your journey from ${j.origin} to ${j.destination} has been cancelled.`,
      type: status === 'completed' ? 'success' : status === 'cancelled' ? 'warning' : 'info',
      read: false,
      timestamp: new Date().toISOString(),
      journeyId: id,
    };
    setNotifications((prev) => [notif, ...prev]);
  };

  const updateJourneyStatus = async (id: string, status: JourneyStatus, by = 'You'): Promise<void> => {
    try {
      let updated: Journey | undefined;
      if (status === 'cancelled') {
        updated = by === 'Admin'
          ? await api.adminCancelJourney(id)
          : await api.cancelJourney(id);
      } else if (status === 'active') {
        updated = await api.activateJourney(id);
      } else if (status === 'completed') {
        updated = await api.completeJourney(id);
      }

      if (updated) {
        setJourneys((prev) =>
          prev.map((j) => (j.id === id ? { ...updated!, segments: j.segments, timeline: j.timeline } : j)),
        );
        setAdminJourneys((prev) =>
          prev.map((j) => (j.id === id ? { ...updated!, segments: j.segments, timeline: j.timeline } : j)),
        );
        addStatusNotification(id, status);
        return;
      }

      throw new Error('Journey status update returned no payload.');
    } catch (err) {
      toast.error('Status update failed', {
        description: err instanceof Error ? err.message : 'Could not update journey status.',
      });
      return;
    }
  };

  // -------------------------------------------------------------------------
  // Notifications
  // -------------------------------------------------------------------------

  const markNotificationRead = async (id: string): Promise<void> => {
    setNotifications((prev) => prev.map((n) => (n.id === id ? { ...n, read: true } : n)));
    try { await notifApi.markNotificationRead(id); } catch { /* optimistic already applied */ }
  };

  const markAllRead = async (): Promise<void> => {
    setNotifications((prev) => prev.map((n) => ({ ...n, read: true })));
    try { await notifApi.markAllNotificationsRead(); } catch { /* ignore */ }
  };

  const clearBookingResult = () => setLastBookingResult(null);

  const updateProfile = async (fields: { name?: string; vehicle_type?: string }): Promise<void> => {
    const token = getToken();
    if (!token || !user) throw new Error('Not authenticated.');
    await iamUpdateProfile(token, fields);
    const u: User = { ...user, ...fields };
    setUser(u);
    localStorage.setItem('cw_user', JSON.stringify(u));
  };

  return (
    <AppContext.Provider
      value={{
        user,
        isAuthenticated: !!user && !!getToken(),
        login,
        register,
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
        updateProfile,
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
