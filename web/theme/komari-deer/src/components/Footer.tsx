"use client";

import { Github, Mail, Send } from 'lucide-react';
import { useState } from 'react';

const Footer = () => {
  const [emailCopied, setEmailCopied] = useState(false);

  const handleEmailClick = async () => {
    try {
      await navigator.clipboard.writeText('601560588@qq.com');
      setEmailCopied(true);
      setTimeout(() => setEmailCopied(false), 2000);
    } catch (err) {
      console.error('Failed to copy email:', err);
    }
  };

  return (
    <footer className="relative overflow-hidden border-t border-white/[0.05] bg-[#0a0e1a]">
      <div className="container relative mx-auto px-8 py-6">
        {/* Social icons */}
        <div className="flex items-center justify-center gap-3 mb-4">
          <a
            href="https://github.com/dengyie"
            target="_blank"
            rel="noreferrer"
            className="flex h-9 w-9 items-center justify-center rounded-lg border border-white/10 bg-white/5 backdrop-blur-sm transition-all hover:border-cyan-400/50 hover:bg-cyan-400/10"
          >
            <Github className="h-4 w-4 text-white/70" />
          </a>
          <a
            href="https://t.me/mangoqwqtech"
            target="_blank"
            rel="noreferrer"
            title="Telegram"
            className="flex h-9 w-9 items-center justify-center rounded-lg border border-white/10 bg-white/5 backdrop-blur-sm transition-all hover:border-cyan-400/50 hover:bg-cyan-400/10"
          >
            <Send className="h-4 w-4 text-white/70" />
          </a>
          <button
            onClick={handleEmailClick}
            className="relative flex h-9 w-9 items-center justify-center rounded-lg border border-white/10 bg-white/5 backdrop-blur-sm transition-all hover:border-cyan-400/50 hover:bg-cyan-400/10 cursor-pointer"
            title="Click to copy email"
          >
            <Mail className="h-4 w-4 text-white/70" />
            {emailCopied && (
              <span className="absolute left-full ml-2 px-3 py-1.5 text-xs font-medium text-cyan-300 bg-white/10 border border-cyan-400/30 backdrop-blur-md rounded-lg shadow-lg whitespace-nowrap animate-in fade-in slide-in-from-left-2 duration-200">
                ✓ Copied!
              </span>
            )}
          </button>
        </div>

        {/* Bottom text */}
        <p className="text-center text-xs text-white/50">
          <a href="https://github.com/dengyie/komari-deer" target="_blank" rel="noreferrer" className="font-semibold text-white/70 hover:text-cyan-400 transition-colors">Komari Deer</a>
          {' · '}
          <span>Where monitoring meets elegance</span>
          {' · '}
          <span>Built with <a href="https://github.com/tonyliuzj/komari-next" target="_blank" rel="noreferrer" className="text-cyan-400 hover:underline">komari-next</a> & ❤️</span>
        </p>
      </div>
    </footer>
  );
};

export default Footer;
