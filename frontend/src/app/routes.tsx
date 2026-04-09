import { createBrowserRouter, Navigate, Outlet, useNavigate } from 'react-router';
import { useEffect } from 'react';
import { AppLayout } from './components/layout/AppLayout';
import { useApp } from './context/AppContext';

// Auth
import LoginPage from './pages/LoginPage';

// Driver pages
import BookJourneyPage from './pages/driver/BookJourneyPage';
import BookingResultPage from './pages/driver/BookingResultPage';
import DriverDashboard from './pages/driver/DashboardPage';
import JourneyDetailPage from './pages/driver/JourneyDetailPage';
import MyJourneysPage from './pages/driver/MyJourneysPage';
import NotificationsPage from './pages/driver/NotificationsPage';
import SettingsPage from './pages/driver/SettingsPage';

// Admin pages
import AdminDashboardPage from './pages/admin/AdminDashboardPage';
import AdminJourneyDetailPage from './pages/admin/AdminJourneyDetailPage';
import AdminNotificationsPage from './pages/admin/AdminNotificationsPage';
import AdminSettingsPage from './pages/admin/AdminSettingsPage';
import AllJourneysPage from './pages/admin/AllJourneysPage';
import AnalyticsPage from './pages/admin/AnalyticsPage';
import EnforcementPage from './pages/admin/EnforcementPage';
import SegmentClosuresPage from './pages/admin/SegmentClosuresPage';
import TrafficMapPage from './pages/admin/TrafficMapPage';

function RequireAuth({ role }: { role: 'admin' | 'driver' }) {
  const { isAuthenticated, user } = useApp();
  const navigate = useNavigate();

  useEffect(() => {
    if (!isAuthenticated || !user) {
      navigate('/auth', { replace: true });
    } else if (user.role !== role) {
      navigate(user.role === 'admin' ? '/admin' : '/driver', { replace: true });
    }
  }, [isAuthenticated, user, role, navigate]);

  if (!isAuthenticated || !user || user.role !== role) return null;
  return <Outlet />;
}

function NotFound() {
  return (
    <div className="min-h-screen flex items-center justify-center p-8" style={{ background: '#F8F6F2' }}>
      <div className="text-center max-w-md">
        <div
          className="w-16 h-16 rounded-full flex items-center justify-center mx-auto mb-5"
          style={{ background: '#F0EDE7' }}
        >
          <span style={{ fontSize: '2rem' }}>🗺️</span>
        </div>
        <h1 style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', marginBottom: '8px' }}>
          Page not found
        </h1>
        <p style={{ color: '#4E5953', marginBottom: '24px', fontSize: '0.9375rem' }}>
          This page doesn't exist. Check the URL or navigate back to safety.
        </p>
        <a
          href="/auth"
          className="px-5 py-2.5 rounded-lg text-white text-sm inline-block"
          style={{ background: '#2F6B55', fontWeight: 600 }}
        >
          Go to sign in
        </a>
      </div>
    </div>
  );
}

export const router = createBrowserRouter([
  {
    path: '/',
    element: <Navigate to="/auth" replace />,
  },
  {
    path: '/auth',
    Component: LoginPage,
  },
  // Driver area - guarded: must be authenticated as driver
  {
    path: '/driver',
    element: <RequireAuth role="driver" />,
    children: [
      {
        Component: AppLayout,
        children: [
          { index: true, Component: DriverDashboard },
          { path: 'book', Component: BookJourneyPage },
          { path: 'booking-result', Component: BookingResultPage },
          { path: 'journeys', Component: MyJourneysPage },
          { path: 'journeys/:id', Component: JourneyDetailPage },
          { path: 'notifications', Component: NotificationsPage },
          { path: 'settings', Component: SettingsPage },
        ],
      },
    ],
  },
  // Admin area - guarded: must be authenticated as admin
  {
    path: '/admin',
    element: <RequireAuth role="admin" />,
    children: [
      {
        Component: AppLayout,
        children: [
          { index: true, Component: AdminDashboardPage },
          { path: 'journeys', Component: AllJourneysPage },
          { path: 'journeys/:id', Component: AdminJourneyDetailPage },
          { path: 'analytics', Component: AnalyticsPage },
          { path: 'enforcement', Component: EnforcementPage },
          { path: 'closures', Component: SegmentClosuresPage },
          { path: 'map', Component: TrafficMapPage },
          { path: 'notifications', Component: AdminNotificationsPage },
          { path: 'settings', Component: AdminSettingsPage },
        ],
      },
    ],
  },
  {
    path: '*',
    Component: NotFound,
  },
]);
