import { NavLink } from 'react-router';
import { useApp } from '../../context/AppContext';
import { LayoutDashboard, Navigation, List, Bell, Settings, BarChart2, Map, Construction } from 'lucide-react';

const driverNav = [
  { to: '/driver', label: 'Home', icon: LayoutDashboard, end: true },
  { to: '/driver/book', label: 'Book', icon: Navigation },
  { to: '/driver/journeys', label: 'Journeys', icon: List },
  { to: '/driver/notifications', label: 'Alerts', icon: Bell },
  { to: '/driver/settings', label: 'Settings', icon: Settings },
];

const adminNav = [
  { to: '/admin', label: 'Home', icon: LayoutDashboard, end: true },
  { to: '/admin/journeys', label: 'Journeys', icon: List },
  { to: '/admin/analytics', label: 'Analytics', icon: BarChart2 },
  { to: '/admin/closures', label: 'Closures', icon: Construction },
  { to: '/admin/map', label: 'Map', icon: Map },
  { to: '/admin/notifications', label: 'Alerts', icon: Bell },
];

export function MobileNav() {
  const { user, unreadCount } = useApp();
  const items = user?.role === 'admin' ? adminNav : driverNav;

  return (
    <nav
      className="lg:hidden fixed bottom-0 left-0 right-0 z-30 bg-white flex"
      style={{ borderTop: '1px solid var(--border)', paddingBottom: 'env(safe-area-inset-bottom)' }}
      aria-label="Mobile navigation"
    >
      {items.map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          end={'end' in item ? item.end : false}
          className="flex-1 flex flex-col items-center gap-1 py-2.5 relative"
          style={({ isActive }) => ({
            color: isActive ? '#2F6B55' : '#4E5953',
          })}
        >
          {({ isActive }) => (
            <>
              <div className="relative">
                <item.icon
                  size={20}
                  strokeWidth={isActive ? 2.25 : 1.75}
                  color={isActive ? '#2F6B55' : '#4E5953'}
                />
                {item.label === 'Alerts' && unreadCount > 0 && (
                  <span
                    className="absolute -top-1.5 -right-1.5 w-4 h-4 rounded-full flex items-center justify-center text-white"
                    style={{ background: '#B42318', fontSize: '0.6rem', fontWeight: 700 }}
                  >
                    {unreadCount > 9 ? '9+' : unreadCount}
                  </span>
                )}
              </div>
              <span
                style={{
                  fontSize: '0.6875rem',
                  fontFamily: 'var(--font-body)',
                  fontWeight: isActive ? 600 : 400,
                }}
              >
                {item.label}
              </span>
              {isActive && (
                <span
                  className="absolute top-0 left-1/2 -translate-x-1/2 w-6 h-0.5 rounded-full"
                  style={{ background: '#2F6B55' }}
                />
              )}
            </>
          )}
        </NavLink>
      ))}
    </nav>
  );
}
