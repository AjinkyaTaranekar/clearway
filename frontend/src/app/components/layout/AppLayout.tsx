import { Analytics } from '@vercel/analytics/react';
import { Bell, Menu } from 'lucide-react';
import { useState } from 'react';
import { Outlet, useNavigate } from 'react-router';
import { useApp } from '../../context/AppContext';
import { MobileNav } from './MobileNav';
import { Sidebar } from './Sidebar';

export function AppLayout() {
  const [sidebarOpen, setSidebarOpen] = useState(false);
  const { unreadCount, user } = useApp();
  const navigate = useNavigate();

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
          <Outlet />
        </main>
      </div>

      {/* Mobile bottom navigation */}
      <MobileNav />
      <Analytics />
    </div>
  );
}
