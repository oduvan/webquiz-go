import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react';

export type MessageType = 'success' | 'error' | 'warning';
export type ShowMessage = (text: string, type: MessageType) => void;

interface Message {
  id: number;
  text: string;
  type: MessageType;
}

const MessageContext = createContext<ShowMessage>(() => undefined);

// The admin page's messages (#message-area). Each one goes away after
// 5 seconds, like on the old page. They are drawn above the pop-ups so the
// admin also sees them while the editor is open, and they never take clicks.
export function MessageProvider({ children }: { children: ReactNode }) {
  const [messages, setMessages] = useState<Message[]>([]);
  const lastId = useRef(0);
  const timers = useRef<ReturnType<typeof setTimeout>[]>([]);

  useEffect(
    () => () => {
      timers.current.forEach((timer) => clearTimeout(timer));
    },
    [],
  );

  const show = useCallback<ShowMessage>((text, type) => {
    lastId.current += 1;
    const id = lastId.current;
    setMessages((list) => list.concat({ id, text: String(text), type }));
    timers.current.push(
      setTimeout(() => {
        setMessages((list) => list.filter((message) => message.id !== id));
      }, 5000),
    );
  }, []);

  return (
    <MessageContext.Provider value={show}>
      {children}
      <div id="message-area" aria-live="polite">
        {messages.map((message) => (
          <div key={message.id} className={'message ' + message.type} role={message.type === 'error' ? 'alert' : 'status'}>
            {message.text}
          </div>
        ))}
      </div>
    </MessageContext.Provider>
  );
}

export function useShowMessage(): ShowMessage {
  return useContext(MessageContext);
}
