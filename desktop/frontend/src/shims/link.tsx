import { forwardRef, type AnchorHTMLAttributes } from "react";
import { Link as RouterLink } from "react-router";

// next/link, for the screens shared with the web app: a link inside the app is the
// router's, and one to anywhere else opens in your browser (see main.tsx).
type Props = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href"> & { href: string; prefetch?: boolean; replace?: boolean; scroll?: boolean };

const Link = forwardRef<HTMLAnchorElement, Props>(function Link({ href, prefetch: _p, replace, scroll: _s, ...rest }, ref) {
  if (/^[a-z]+:/i.test(href) || href.startsWith("/api/")) return <a ref={ref} href={href} {...rest} />;
  return <RouterLink ref={ref} to={href} replace={replace} {...rest} />;
});

export default Link;
