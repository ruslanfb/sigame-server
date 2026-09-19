import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router';
import { Toasts } from './components/Toasts.tsx';
import { HostPage } from './routes/HostPage.tsx';
import { HostRoomPage } from './routes/HostRoomPage.tsx';
import { JoinPage } from './routes/JoinPage.tsx';
import { PlayerRoomPage } from './routes/PlayerRoomPage.tsx';
import { TablePage } from './routes/TablePage.tsx';

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, staleTime: 10_000, refetchOnWindowFocus: false } },
});

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/" element={<JoinPage />} />
          <Route path="/host" element={<HostPage />} />
          <Route path="/room/:code/player" element={<PlayerRoomPage />} />
          <Route path="/room/:code/host" element={<HostRoomPage />} />
          <Route path="/room/:code/table" element={<TablePage />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
        <Toasts />
      </BrowserRouter>
    </QueryClientProvider>
  );
}
