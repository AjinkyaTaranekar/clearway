import { Analytics } from '@vercel/analytics/react';
import { Bell, Menu } from 'lucide-react';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import { Outlet, useNavigate } from 'react-router';
import { useApp } from '../../context/AppContext';
import {
  dismissPushPermissionPrompt,
  enablePushNotifications,
  shouldShowPushPermissionPrompt,
} from '../../services/pushNotifications';
import { MobileNav } from './MobileNav';
import { Sidebar } from './Sidebar';

export function AppLayout() {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const [showPushPrompt, setShowPushPrompt] = useState(false);
  const [pushPromptBusy, setPushPromptBusy] = useState(false);
  const { unreadCount, user } = useApp();
  const navigate = useNavigate();

  useEffect(() => {
    if (user?.role !== 'driver') {
      setShowPushPrompt(false);
      return;
    }
    setShowPushPrompt(shouldShowPushPermissionPrompt());
  }, [user?.id, user?.role]);

  const handleEnablePushPrompt = async () => {
    setPushPromptBusy(true);
    try {
      await enablePushNotifications();
      setShowPushPrompt(false);
      toast.success('Push notifications enabled');
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Unable to enable browser notifications.';
      toast.error('Push setup failed', { description: message });

      const lower = message.toLowerCase();
      if (lower.includes('not granted') || lower.includes('blocked')) {
        dismissPushPermissionPrompt();
        setShowPushPrompt(false);
      }
    } finally {
      setPushPromptBusy(false);
    }
  };

  const handleDismissPushPrompt = () => {
    dismissPushPermissionPrompt();
    setShowPushPrompt(false);
  };

  return (
    <div className="flex h-screen overflow-hidden" style={{ background: 'var(--background)' }}>
      {/* Desktop sidebar */}
      <Sidebar open={sidebarOpen} onClose={() => setSidebarOpen(false)} />

      {/* Main content area */}
      <div className="flex-1 flex flex-col min-w-0 overflow-hidden">
        {/* Top header */}
        <header
          className="flex items-center justify-between px-4 lg:px-6 py-3 bg-white flex-shrink-0"
          style={{ borderBottom: '1px solid var(--border)' }}
        >
          <div className="flex items-center gap-3">
            {/* Mobile hamburger */}
            <button
              onClick={() => setSidebarOpen(true)}
              className="lg:hidden p-2 rounded-lg hover:bg-muted transition-colors"
              aria-label="Open menu"
            >
              <Menu size={20} color="#4E5953" />
            </button>

            {/* Mobile logo */}
            <div className="flex items-center gap-2 lg:hidden">
              <div className="w-7 h-7 rounded-lg flex items-center justify-center" style={{ background: '#2F6B55' }}>
                <svg width="14" height="14" viewBox="0 0 16 16" fill="none">
                  <path d="M8 2L13 8L8 14L3 8L8 2Z" fill="white" opacity="0.3" />
                  <path d="M8 4L11.5 8L8 12L4.5 8L8 4Z" fill="white" opacity="0.6" />
                  <circle cx="8" cy="8" r="2" fill="white" />
                </svg>
              </div>
              <span style={{ fontFamily: 'var(--font-heading)', fontWeight: 700, color: '#1F2421', fontSize: '1rem' }}>
                Clearway
              </span>
            </div>

            {/* Desktop breadcrumb placeholder */}
            <div className="hidden lg:flex items-center gap-2">
              <span style={{ color: '#4E5953', fontSize: '0.875rem' }}>
                {user?.role === 'admin' ? 'Admin Console' : 'Driver Portal'}
              </span>
            </div>
          </div>

          <div className="flex items-center gap-2">
            {/* Notifications bell */}
            <button
              onClick={() =>
                navigate(user?.role === 'admin' ? '/admin/notifications' : '/driver/notifications')
              }
              className="relative p-2 rounded-lg hover:bg-muted transition-colors"
              aria-label={`Notifications${unreadCount > 0 ? `, ${unreadCount} unread` : ''}`}
            >
              <Bell size={20} color="#4E5953" />
              {unreadCount > 0 && (
                <span
                  className="absolute top-1 right-1 w-4 h-4 rounded-full flex items-center justify-center text-white"
                  style={{ background: '#B42318', fontSize: '0.6rem', fontWeight: 700 }}
                >
                  {unreadCount > 9 ? '9+' : unreadCount}
                </span>
              )}
            </button>
          </div>
        </header>

        {/* Page content - add bottom padding on mobile for the bottom nav */}
        <main className="flex-1 overflow-y-auto pb-20 lg:pb-0">
          {showPushPrompt && (
            <div
              className="mx-4 mt-4 lg:mx-6 rounded-lg p-3 flex items-center justify-between gap-3"
              style={{ border: '1px solid var(--border)', background: '#F0EDE7' }}
            >
              <div>
                <div style={{ color: '#1F2421', fontWeight: 600, fontSize: '0.875rem' }}>
                  Enable trip alerts
                </div>
                <div style={{ color: '#4E5953', fontSize: '0.8125rem' }}>
                  Turn on browser notifications to get instant journey updates.
                </div>
              </div>
              <div className="flex items-center gap-2 flex-shrink-0">
                <button
                  type="button"
                  onClick={handleDismissPushPrompt}
                  className="px-3 py-1.5 rounded-lg text-sm"
                  style={{ border: '1px solid var(--border)', color: '#4E5953', background: 'white' }}
                >
                  Not now
                </button>
                <button
                  type="button"
                  onClick={() => { void handleEnablePushPrompt(); }}
                  disabled={pushPromptBusy}
                  className="px-3 py-1.5 rounded-lg text-sm text-white"
                  style={{ background: '#2F6B55', opacity: pushPromptBusy ? 0.7 : 1 }}
                >
                  {pushPromptBusy ? 'Enabling...' : 'Enable'}
                </button>
              </div>
            </div>
          )}
          <Outlet />
        </main>
      </div>

      {/* Mobile bottom navigation */}
      <MobileNav />
      <Analytics />
    </div>
  );
}
