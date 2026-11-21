import { useMemo } from "react";
import { useLocation, useNavigate, useParams as useRouterParams } from "react-router";

// next/navigation, for the screens shared with the web app, on the app's router.

export function useRouter() {
  const navigate = useNavigate();
  return useMemo(
    () => ({
      push: (to: string) => navigate(to),
      replace: (to: string) => navigate(to, { replace: true }),
      back: () => navigate(-1),
      refresh: () => {},
      prefetch: (_: string) => {},
    }),
    [navigate],
  );
}

export function usePathname() {
  return useLocation().pathname;
}

export function useSearchParams() {
  const { search } = useLocation();
  return useMemo(() => new URLSearchParams(search), [search]);
}

export function useParams<T extends Record<string, string>>() {
  return useRouterParams() as T;
}
