import { NavLink, useNavigate } from 'react-router';
import { useApp } from '../../context/AppContext';
import {
  LayoutDashboard,
  Navigation,
  List,
  Bell,
  Settings,
  LogOut,
  BarChart2,
  Map,
  ShieldCheck,
  X,
} from 'lucide-react';

const driverNav = [
  { to: '/driver', label: 'Dashboard', icon: LayoutDashboard, end: true },
  { to: '/driver/book', label: 'Book journey', icon: Navigation, end: false },
  { to: '/driver/journeys', label: 'My journeys', icon: List, end: false },
  { to: '/driver/notifications', label: 'Notifications', icon: Bell, end: false },
  { to: '/driver/settings', label: 'Settings', icon: Settings, end: false },
];

const adminNav = [
  { to: '/admin', label: 'Dashboard', icon: LayoutDashboard, end: true },
  { to: '/admin/journeys', label: 'All journeys', icon: List, end: false },
  { to: '/admin/analytics', label: 'Analytics', icon: BarChart2, end: false },
  { to: '/admin/enforcement', label: 'Enforcement', icon: ShieldCheck, end: false },
  { to: '/admin/map', label: 'Traffic map', icon: Map, end: false },
  { to: '/admin/notifications', label: 'Notifications', icon: Bell, end: false },
  { to: '/admin/settings', label: 'Settings', icon: Settings, end: false },
];

interface SidebarProps {
  open: boolean;
  onClose: () => void;
}

export function Sidebar({ open, onClose }: SidebarProps) {
  const { user, logout, unreadCount } = useApp();
  const navigate = useNavigate();
  const navItems = user?.role === 'admin' ? adminNav : driverNav;

  const handleLogout = () => {
    logout();
    navigate('/');
  };

  const initials = user?.name
    .split(' ')
    .map((n) => n[0])
    .join('')
    .toUpperCase();

  return (
    <>
      {/* Mobile overlay */}
      {open && (
        <div
          className="fixed inset-0 z-40 bg-black/20 lg:hidden"
          onClick={onClose}
          aria-hidden="true"
        />
      )}

      {/* Sidebar panel */}
      <aside
        style={{ borderRight: '1px solid var(--border)' }}
        className={`
          fixed top-0 left-0 z-50 h-full w-64 bg-white flex flex-col
          transition-transform duration-300 ease-in-out
          lg:static lg:translate-x-0 lg:z-auto lg:flex-shrink-0
          ${open ? 'translate-x-0' : '-translate-x-full'}
        `}
      >
        {/* Logo */}
        <div
          className="flex items-center justify-between px-5 py-5"
          style={{ borderBottom: '1px solid var(--border)' }}
        >
          <div className="flex items-center gap-2.5">
            <div
              className="w-8 h-8 rounded-lg flex items-center justify-center flex-shrink-0"
              style={{ background: '#2F6B55' }}
            >
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
                <path d="M8 2L13 8L8 14L3 8L8 2Z" fill="white" opacity="0.3" />
                <path d="M8 4L11.5 8L8 12L4.5 8L8 4Z" fill="white" opacity="0.6" />
                <circle cx="8" cy="8" r="2" fill="white" />
              </svg>
            </div>
            <div>
              <div
                style={{
                  fontFamily: 'var(--font-heading)',
                  fontWeight: 700,
                  fontSize: '1rem',
                  color: '#1F2421',
                  lineHeight: 1.2,
                }}
              >
                Clearway
              </div>
              <div style={{ fontSize: '0.65rem', color: '#4E5953', fontWeight: 500, letterSpacing: '0.04em', textTransform: 'uppercase' }}>
                {user?.role === 'admin' ? 'Admin Console' : 'Driver Portal'}
              </div>
            </div>
          </div>
          <button
            onClick={onClose}
            className="lg:hidden p-1 rounded-md hover:bg-muted transition-colors"
            aria-label="Close menu"
          >
            <X size={18} color="#4E5953" />
          </button>
        </div>

        {/* Navigation */}
        <nav className="flex-1 px-3 py-4 overflow-y-auto">
          <ul className="space-y-0.5" role="list">
            {navItems.map((item) => (
              <li key={item.to}>
                <NavLink
                  to={item.to}
                  end={item.end}
                  onClick={onClose}
                  className={({ isActive }) => `
                    flex items-center gap-3 px-3 py-2.5 rounded-lg transition-colors text-sm
                    ${isActive
                      ? 'text-white'
                      : 'hover:bg-muted'
                    }
                  `}
                  style={({ isActive }) => ({
                    background: isActive ? '#2F6B55' : 'transparent',
                    color: isActive ? 'white' : '#1F2421',
                    fontWeight: isActive ? 500 : 400,
                  })}
                >
                  {({ isActive }) => (
                    <>
                      <item.icon
                        size={17}
                        color={isActive ? 'white' : '#4E5953'}
                        strokeWidth={isActive ? 2 : 1.75}
                      />
                      <span className="flex-1">{item.label}</span>
                      {item.label === 'Notifications' && unreadCount > 0 && (
                        <span
                          className="text-xs rounded-full px-1.5 py-0.5 min-w-[20px] text-center"
                          style={{
                            background: isActive ? 'rgba(255,255,255,0.25)' : '#2F6B55',
                            color: isActive ? 'white' : 'white',
                            fontSize: '0.7rem',
                            fontWeight: 600,
                          }}
                        >
                          {unreadCount}
                        </span>
                      )}
                    </>
                  )}
                </NavLink>
              </li>
            ))}
          </ul>
        </nav>

        {/* User section */}
        <div
          className="px-3 py-3"
          style={{ borderTop: '1px solid var(--border)' }}
        >
          <div className="flex items-center gap-3 px-3 py-2 rounded-lg">
            <div
              className="w-8 h-8 rounded-full flex items-center justify-center flex-shrink-0 text-white text-xs"
              style={{ background: '#2F6B55', fontWeight: 600, fontFamily: 'var(--font-heading)' }}
            >
              {initials}
            </div>
            <div className="flex-1 min-w-0">
              <div
                className="truncate text-sm"
                style={{ color: '#1F2421', fontWeight: 500 }}
              >
                {user?.name}
              </div>
              <div
                className="truncate text-xs"
                style={{ color: '#4E5953' }}
              >
                {user?.email}
              </div>
            </div>
          </div>
          <button
            onClick={handleLogout}
            className="w-full flex items-center gap-3 px-3 py-2.5 rounded-lg hover:bg-muted transition-colors mt-1"
            style={{ color: '#4E5953' }}
          >
            <LogOut size={16} />
            <span className="text-sm">Sign out</span>
          </button>
        </div>
      </aside>
    </>
  );
}
