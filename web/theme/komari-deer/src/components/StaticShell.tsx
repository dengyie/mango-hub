// 预水合静态壳:SSG 与客户端首帧渲染完全一致(纯静态,无 hooks/无数据/无 context),
// 修补路由 pending 渲染 null 造成的空 main 白屏(深 review Finding 1,FCP 回归)。
// 刻意不用 .dashboard-skeleton 类——硬加载详情页先看到的是中性壳,不闪仪表盘骨架(I1 断言依赖该类)。
// role=status + aria-busy:慢水合窗口(最长 ~1 分钟)读屏器能感知加载中。
const StaticShell = () => (
  <div
    className="app-static-shell flex min-h-screen w-full items-center justify-center bg-[#0a0e1a]"
    role="status"
    aria-busy="true"
  >
    <div className="flex flex-col items-center gap-4">
      <div className="h-2 w-24 rounded-full bg-cyan-400/60 animate-pulse" />
      <p className="text-xs font-medium tracking-widest text-white/40">Mango Hub</p>
    </div>
  </div>
);

export default StaticShell;
