import Link from "next/link";

export function Footer() {
  return (
    <footer className="rule-t mt-auto bg-bg/60">
      <div className="mx-auto flex h-14 max-w-[1200px] items-center justify-center px-5 sm:px-8">
        <p className="font-mono text-[12px] text-dim">
          Made by{" "}
          <Link
            href="https://x.com/prasadtwts"
            target="_blank"
            rel="noreferrer noopener"
            className="text-muted transition-colors hover:text-accent"
          >
            Prasad
          </Link>{" "}
          &amp;{" "}
          <Link
            href="https://x.com/ssh_ashish"
            target="_blank"
            rel="noreferrer noopener"
            className="text-muted transition-colors hover:text-accent"
          >
            Ashish
          </Link>
        </p>
      </div>
    </footer>
  );
}
