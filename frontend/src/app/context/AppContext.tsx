import React, { createContext, useContext, useEffect, useMemo, useState } from 'react';
import { Journey, JourneyStatus, mockJourneys, mockNotifications, Notification } from '../data/mockData';
import { clearTokens, getRefreshToken, getToken, storeTokens } from '../services/auth';
import { iamLogin, iamLogout, iamRegister, iamUpdateProfile, RegisterParams } from '../services/iamApi';
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

/** Data collected by the registration form. */
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
  /**
   * Authenticate via the IAM service.
   * Returns the role assigned by the server so the caller can navigate correctly.
   */
  login: (email: string, password: string) => Promise<UserRole>;
  /**
   * Register a new driver account via the IAM service.
   * Returns the role (always "driver" for new accounts).
   */
  register: (data: RegisterData) => Promise<UserRole>;
  /** Revoke tokens on the IAM service and clear local session. */
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
  /**
   * Update the authenticated driver's name (and optionally vehicle type) via
   * the IAM profile endpoint, then sync local user state and localStorage.
   */
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

  // Refresh journeys/notifications when the user changes.
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
      } catch {
        // API unavailable — keep mock data shown
        if (user.role === 'driver') setJourneys(mockJourneys);
        else setAdminJourneys(mockJourneys);
      }

      try {
        const res = await notifApi.listNotifications();
        setNotifications(res.notifications.map(mapApiNotification));
      } catch {
        setNotifications(mockNotifications);
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
      try {
        await iamLogout(accessToken, refreshToken);
      } catch {
        // best-effort
      }
    }
  };

  // -------------------------------------------------------------------------
  // Journeys (with mock fallback)
  // -------------------------------------------------------------------------

  const bookJourneyMock = (data: BookingData): BookingResult => {
    const rejected = Math.random() < 0.3;
    const journeyId = `J${Date.now()}`;
    const rejectionReason = rejected ? 'Capacity is full on one or more segments.' : undefined;

    const newJourney: Journey = {
      id: journeyId,
      driverId: user?.id ?? 'D001',
      driverName: user?.name ?? 'Driver',
      origin: data.origin,
      destination: data.destination,
      departureTime: data.departureTime,
      estimatedArrival: data.departureTime,
      vehicleType: data.vehicleType as Journey['vehicleType'],
      status: rejected ? 'rejected' : 'approved',
      region: 'Central',
      rejectionReason,
      segments: rejected
        ? []
        : [
            { id: 'S1', name: 'Main Street', occupancy: 42, level: 'low' },
            { id: 'S2', name: 'Ring Road', occupancy: 58, level: 'medium' },
          ],
      timeline: [
        { id: 'T1', type: 'created', label: 'Journey booked', timestamp: new Date().toISOString(), by: 'You' },
        {
          id: 'T2',
          type: rejected ? 'rejected' : 'approved',
          label: rejected ? 'Journey rejected' : 'Journey approved',
          timestamp: new Date().toISOString(),
          by: 'System',
        },
      ],
      createdAt: new Date().toISOString(),
      updatedAt: new Date().toISOString(),
      distance: `${(Math.random() * 15 + 5).toFixed(1)} km`,
      duration: `${Math.floor(Math.random() * 30 + 20)} min`,
    };

    setJourneys((prev) => [newJourney, ...prev]);
    setAdminJourneys((prev) => [newJourney, ...prev]);

    const notif: Notification = {
      id: `N${Date.now()}`,
      title: rejected ? 'Journey rejected' : 'Journey approved',
      message: rejected
        ? `Your journey from ${data.origin} to ${data.destination} could not be booked. ${rejectionReason}`
        : `Your journey from ${data.origin} to ${data.destination} has been approved. Activate it at departure time.`,
      type: rejected ? 'error' : 'success',
      read: false,
      timestamp: new Date().toISOString(),
      journeyId,
    };
    setNotifications((prev) => [notif, ...prev]);

    const result: BookingResult = { success: !rejected, journeyId, reason: rejectionReason, journey: newJourney };
    setLastBookingResult(result);
    return result;
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

      // Best-effort refresh notifications after booking
      setTimeout(async () => {
        try {
          const res = await notifApi.listNotifications();
          setNotifications(res.notifications.map(mapApiNotification));
        } catch {
          // ignore
        }
      }, 1500);

      return result;
    } catch (err) {
      // U-06: Surface the real error instead of silently falling back to mock data.
      // The driver sees the actual reason (network failure, capacity rejection, etc.)
      // rather than a fabricated approval/rejection result.
      const reason = err instanceof Error ? err.message : 'Booking request failed. Please try again.';
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
        status === 'active'
          ? 'Journey started'
          : status === 'completed'
            ? 'Journey completed'
            : 'Journey cancelled',
      message:
        status === 'active'
          ? `Your journey from ${j.origin} to ${j.destination} is now active. Drive safely.`
          : status === 'completed'
            ? `Your journey from ${j.origin} to ${j.destination} has been completed.`
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
        // U-07: route admin force-cancel to the correct admin endpoint (no 30-min restriction).
        updated = by === 'Admin'
          ? await api.adminCancelJourney(id)
          : await api.cancelJourney(id);
      } else if (status === 'active') updated = await api.activateJourney(id);
      else if (status === 'completed') updated = await api.completeJourney(id);

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
    } catch {
      // fall through to mock update
    }

    const labelMap: Record<string, string> = {
      active: 'Journey started',
      completed: 'Journey completed',
      cancelled: 'Journey cancelled',
    };
    const updateFn = (list: Journey[]) =>
      list.map((j) => {
        if (j.id !== id) return j;
        return {
          ...j,
          status,
          timeline: [
            ...j.timeline,
            {
              id: `T${Date.now()}`,
              type: status,
              label: labelMap[status] || `Status changed to ${status}`,
              timestamp: new Date().toISOString(),
              by,
            },
          ],
          updatedAt: new Date().toISOString(),
        };
      });
    setJourneys(updateFn);
    setAdminJourneys(updateFn);
    addStatusNotification(id, status);
  };

  // -------------------------------------------------------------------------
  // Notifications
  // -------------------------------------------------------------------------

  const markNotificationRead = async (id: string): Promise<void> => {
    setNotifications((prev) => prev.map((n) => (n.id === id ? { ...n, read: true } : n)));
    try {
      await notifApi.markNotificationRead(id);
    } catch {
      // ignore — optimistic update already applied
    }
  };

  const markAllRead = async (): Promise<void> => {
    setNotifications((prev) => prev.map((n) => ({ ...n, read: true })));
    try {
      await notifApi.markAllNotificationsRead();
    } catch {
      // ignore
    }
  };

  const clearBookingResult = () => setLastBookingResult(null);

  // U-01 / U-11: Persist profile changes to the IAM service and keep local
  // state in sync.  Currently supports name and vehicle_type — the two fields
  // that IAM's PUT /api/v1/auth/profile accepts.  Email changes require a
  // separate IAM flow (out of scope for the prototype).
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
        isAuthenticated: !!user,
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
