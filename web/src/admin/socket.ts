import type { Json } from './api';

// The admin WebSocket (/ws/admin): students waiting for approval, the tunnel
// status, file transfer requests and "answers shown" news. After a lost
// connection it connects again in 5 seconds, like the old page. Returns a
// function that closes the socket for good.
export function connectAdminSocket(onMessage: (data: Json) => void): () => void {
  let socket: WebSocket | null = null;
  let stopped = false;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const connect = () => {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    socket = new WebSocket(`${protocol}//${window.location.host}/ws/admin`);
    socket.onmessage = (event) => {
      try {
        onMessage(JSON.parse(event.data));
      } catch (error) {
        console.error('Error parsing admin WebSocket message:', error);
      }
    };
    socket.onclose = () => {
      if (!stopped) timer = setTimeout(connect, 5000);
    };
    socket.onerror = (error) => {
      console.error('Admin WebSocket error:', error);
    };
  };

  connect();
  return () => {
    stopped = true;
    if (timer !== undefined) clearTimeout(timer);
    if (socket) socket.close();
  };
}
