import React, { createContext, useContext, useEffect, useState } from 'react';
import {
  allJourneys as allJourneysData,
  Journey,
  JourneyStatus,
  mockJourneys,
  mockNotifications,
  Notification,
} from '../data/mockData';
import { generateJWT, clearToken } from '../services/auth';
import * as api from '../services/journeyApi';

export type UserRole = 'driver' | 'admin';

export interface User {
  id: string;
  name: string;
  email: string;
  role: UserRole;
  phone?: string;
  vehicleId?: string;
  avatar?: string;
}

export interface BookingData {
  origin: string;
  destination: string;
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
  login: (email: string, password: string, role: UserRole) => void;
  logout: () => void;
  journeys: Journey[];
  adminJourneys: Journey[];
  notifications: Notification[];
  unreadCount: number;
  lastBookingResult: BookingResult | null;
  bookJourney: (data: BookingData) => Promise<BookingResult>;
  updateJourneyStatus: (id: string, status: JourneyStatus, by?: string) => Promise<void>;
  markNotificationRead: (id: string) => void;
  markAllRead: () => void;
  clearBookingResult: () => void;
}

const AppContext = createContext<AppContextType | null>(null);

export function AppProvider({ children }: { children: React.ReactNode }) {
  const [user, setUser] = useState<User | null>(() => {
    try {
      const stored = localStorage.getItem('cw_user');
      return stored ? JSON.parse(stored) : null;
    } catch {
      return null;
    }
  });

  const [journeys, setJourneys] = useState<Journey[]>(mockJourneys);
  const [adminJourneys, setAdminJourneys] = useState<Journey[]>(allJourneysData);
  const [notifications, setNotifications] = useState<Notification[]>(mockNotifications);
  const [lastBookingResult, setLastBookingResult] = useState<BookingResult | null>(null);

  const unreadCount = notifications.filter((n) => !n.read).length;

  // Refresh journeys from API when user logs in
  useEffect(() => {
    if (!user) return;
    const load = async () => {
      try {
        if (user.role === 'driver') {
          const { journeys: apiJourneys } = await api.listJourneys(user.id);
          if (apiJourneys.length > 0) setJourneys(apiJourneys);
        } else if (user.role === 'admin') {
          const { journeys: apiJourneys } = await api.adminListJourneys();
          if (apiJourneys.length > 0) setAdminJourneys(apiJourneys);
        }
      } catch {
        // API unavailable — keep mock data
      }
    };
    load();
  }, [user?.id]);

  const login = (email: string, _password: string, role: UserRole) => {
    const u: User = {
      id: role === 'driver' ? 'D001' : 'A001',
      name: role === 'driver' ? 'Alex Chen' : 'Sarah Mitchell',
      email,
      role,
      phone: role === 'driver' ? '+44 7700 900 123' : '+44 7700 900 456',
      vehicleId: role === 'driver' ? 'VH-4821' : undefined,
    };
    setUser(u);
    localStorage.setItem('cw_user', JSON.stringify(u));
    // Generate JWT for API calls (fire-and-forget — login UX is not blocked)
    generateJWT(u.id, role).catch(() => {});
  };

  const logout = () => {
    setUser(null);
    localStorage.removeItem('cw_user');
    clearToken();
  };

  const bookJourney = async (data: BookingData): Promise<BookingResult> => {
    // Try real API first
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
      const notif: Notification = {
        id: `N${Date.now()}`,
        title: result.success ? 'Journey approved' : 'Journey rejected',
        message: result.success
          ? `Your journey from ${data.origin} to ${data.destination} has been approved. Activate it at departure time.`
          : `Your journey from ${data.origin} to ${data.destination} could not be booked. ${journey.rejectionReason ?? ''}`,
        type: result.success ? 'success' : 'error',
        read: false,
        timestamp: new Date().toISOString(),
        journeyId: journey.id,
      };
      setNotifications((prev) => [notif, ...prev]);
      setLastBookingResult(result);
      return result;
    } catch {
      // API unavailable — fall back to mock
      return bookJourneyMock(data);
    }
  };

  const bookJourneyMock = (data: BookingData): BookingResult => {
    const hour = new Date(data.departureTime).getHours();
    const isPeakTime = hour >= 7 && hour <= 9;
    const isHighDemandRoute =
      data.destination.toLowerCase().includes('east') ||
      data.origin.toLowerCase().includes('east') ||
      (data.destination.toLowerCase().includes('port') && isPeakTime);

    const rejected = isPeakTime && isHighDemandRoute;
    const journeyId = `J-${2900 + Math.floor(Math.random() * 99)}`;
    const rejectionReason = rejected
      ? 'One or more road segments on your route are at full capacity at this time. Try selecting a later departure - after 09:30 usually has more availability.'
      : undefined;

    const newJourney: Journey = {
      id: journeyId,
      driverId: 'D001',
      driverName: 'Alex Chen',
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
        { id: 'T2', type: rejected ? 'rejected' : 'approved', label: rejected ? 'Journey rejected' : 'Journey approved', timestamp: new Date().toISOString(), by: 'System' },
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

  const updateJourneyStatus = async (id: string, status: JourneyStatus, by = 'You'): Promise<void> => {
    // Try real API
    try {
      let updated: Journey | undefined;
      if (status === 'cancelled') updated = await api.cancelJourney(id);
      else if (status === 'active') updated = await api.activateJourney(id);
      else if (status === 'completed') updated = await api.completeJourney(id);

      if (updated) {
        setJourneys((prev) => prev.map((j) => (j.id === id ? { ...updated!, segments: j.segments, timeline: j.timeline } : j)));
        setAdminJourneys((prev) => prev.map((j) => (j.id === id ? { ...updated!, segments: j.segments, timeline: j.timeline } : j)));
        addStatusNotification(id, status);
        return;
      }
    } catch {
      // Fall through to mock
    }

    // Mock fallback
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
          timeline: [...j.timeline, { id: `T${Date.now()}`, type: status, label: labelMap[status] || `Status changed to ${status}`, timestamp: new Date().toISOString(), by }],
          updatedAt: new Date().toISOString(),
        };
      });
    setJourneys(updateFn);
    setAdminJourneys(updateFn);
    addStatusNotification(id, status);
  };

  const addStatusNotification = (id: string, status: JourneyStatus) => {
    const j = journeys.find((j) => j.id === id);
    if (!j) return;
    const notif: Notification = {
      id: `N${Date.now()}`,
      title: status === 'active' ? 'Journey started' : status === 'completed' ? 'Journey completed' : 'Journey cancelled',
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

  const markNotificationRead = (id: string) => {
    setNotifications((prev) => prev.map((n) => (n.id === id ? { ...n, read: true } : n)));
  };

  const markAllRead = () => {
    setNotifications((prev) => prev.map((n) => ({ ...n, read: true })));
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
