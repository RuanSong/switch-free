import { useEffect, useRef } from "react";
import { Events } from "@wailsio/runtime";

/**
 * 订阅一个 Wails 事件，返回取消订阅函数
 */
export function useWailsEvent(eventName: string, handler: (data: any) => void) {
  const handlerRef = useRef(handler);
  handlerRef.current = handler;

  useEffect(() => {
    const off = Events.On(eventName, (e: any) => {
      // Wails v3 事件数据在 e.data 里
      handlerRef.current(e.data ?? e);
    });
    return () => {
      if (typeof off === "function") off();
    };
  }, [eventName]);
}
