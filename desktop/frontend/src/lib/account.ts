"use client";

import { useEffect, useState } from "react";
import { call, type Account } from "./api";

// This computer's owner, for pages that need them. null until it's known; if Tinio isn't
// answering yet, it's asked again, a little later each time.
export function useAccount(): Account | null {
  const [account, setAccount] = useState<Account | null>(null);
  useEffect(() => {
    let wait = 2000;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const ask = () =>
      call<{ account: Account }>("/me").then((res) => {
        if (res.ok) return setAccount(res.data.account);
        timer = setTimeout(ask, wait);
        wait = Math.min(wait * 2, 30_000);
      });
    ask();
    return () => clearTimeout(timer);
  }, []);
  return account;
}
