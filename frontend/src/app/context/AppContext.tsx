import React, { createContext, useContext, useState } from 'react';
import {
  allJourneys as allJourneysData,
  Journey,
  JourneyStatus,
  mockJourneys,
  mockNotifications,
  Notification,
} from '../data/mockData';

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
  bookJourney: (data: BookingData) => BookingResult;
  updateJourneyStatus: (id: string, status: JourneyStatus, by?: string) => void;
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
  };

  const logout = () => {
    setUser(null);
    localStorage.removeItem('cw_user');
  };

  const bookJourney = (data: BookingData): BookingResult => {
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
        {
          id: 'T1',
          type: 'created',
          label: 'Journey booked',
          timestamp: new Date().toISOString(),
          by: 'You',
        },
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

    const result: BookingResult = {
      success: !rejected,
      journeyId,
      reason: rejectionReason,
      journey: newJourney,
    };
    setLastBookingResult(result);
    return result;
  };

  const updateJourneyStatus = (id: string, status: JourneyStatus, by = 'You') => {
    const labelMap: Record<string, string> = {
      active: 'Journey started',
      completed: 'Journey completed',
      cancelled: 'Journey cancelled',
    };

    const updateFn = (list: Journey[]) =>
      list.map((j) => {
        if (j.id !== id) return j;
        const newTimeline = [
          ...j.timeline,
          {
            id: `T${Date.now()}`,
            type: status,
            label: labelMap[status] || `Status changed to ${status}`,
            timestamp: new Date().toISOString(),
            by,
          },
        ];
        return { ...j, status, timeline: newTimeline, updatedAt: new Date().toISOString() };
      });

    setJourneys(updateFn);
    setAdminJourneys(updateFn);

    const j = journeys.find((j) => j.id === id);
    if (j) {
      const notif: Notification = {
        id: `N${Date.now()}`,
        title: labelMap[status] || 'Journey updated',
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
    }
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
