import { ApiError } from './account';
import type { LiveCommand, LiveMessage } from './quiz-api';
export type ConnectionState = 'connecting' | 'connected' | 'reconnecting' | 'ended';
/** No Vercel rewrite: use the API's direct origin. Refresh the short-lived ticket on EVERY reconnect. */
export function quizSocketURL(token: string, wsUrl?: string): string {
  const origin =
    process.env.NEXT_PUBLIC_API_WS_ORIGIN ||
    (process.env.NODE_ENV === 'development'
      ? 'ws://localhost:8080'
      : 'wss://studenthub.85-137-91-47.sslip.io');
  const u = wsUrl ? new URL(wsUrl) : new URL('/api/quiz/live/ws', origin);
  u.protocol = u.protocol === 'http:' || u.protocol === 'ws:' ? 'ws:' : 'wss:';
  u.searchParams.set('token', token);
  return u.toString();
}
export function connectQuizLive(
  getTicket: () => Promise<{ token: string; wsUrl: string }>,
  receive: (m: LiveMessage) => void,
  status: (s: ConnectionState) => void,
) {
  let socket: WebSocket | undefined,
    timer: ReturnType<typeof setTimeout> | undefined,
    stopped = false,
    attempts = 0;
  const end = () => {
    stopped = true;
    clearTimeout(timer);
    socket?.close();
    status('ended');
  };
  const retry = () => {
    if (stopped) return;
    status('reconnecting');
    timer = setTimeout(() => void open(), Math.min(1000 * 2 ** attempts++, 15000) + Math.random() * 300);
  };
  const open = async () => {
    if (stopped) return;
    status(attempts ? 'reconnecting' : 'connecting');
    try {
      const { token, wsUrl } = await getTicket();
      if (stopped) return;
      socket = new WebSocket(quizSocketURL(token, wsUrl));
      socket.onopen = () => {
        attempts = 0;
        status('connected');
      };
      socket.onmessage = (e) => {
        let m: LiveMessage;
        try {
          m = JSON.parse(e.data);
        } catch {
          return;
        }
        receive(m);
        if (m.type === 'ended' || m.type === 'kicked') end();
      };
      socket.onclose = (e) => {
        if (e.code === 4004 || e.code === 4003) end();
        else retry();
      };
      socket.onerror = () => socket?.close();
    } catch (e) {
      if (e instanceof ApiError && [401, 403, 404, 410].includes(e.status)) end();
      else retry();
    }
  };
  void open();
  return {
    send: (m: LiveCommand) => {
      if (socket?.readyState !== WebSocket.OPEN || stopped) return false;
      socket.send(JSON.stringify(m));
      return true;
    },
    close: () => {
      stopped = true;
      clearTimeout(timer);
      socket?.close();
    },
  };
}
