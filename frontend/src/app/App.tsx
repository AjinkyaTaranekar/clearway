import { RouterProvider } from 'react-router';
import { router } from './routes';
import { AppProvider } from './context/AppContext';
import { Toaster } from 'sonner';

export default function App() {
  return (
    <AppProvider>
      <RouterProvider router={router} />
      <Toaster
        position="top-right"
        toastOptions={{
          style: {
            fontFamily: 'var(--font-body)',
            border: '1px solid var(--border)',
            borderRadius: '10px',
          },
        }}
      />
    </AppProvider>
  );
}
