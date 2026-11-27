async (page) => {
  await page.goto("about:blank");
  await page.mouse.move(0, 0);
  await page.unrouteAll({ behavior: "wait" });
  const qa = { requests: [], network: [], errors: [], metadataFailure: false, historyFailure: false, liveFailure: false, historyDelay: 0 };
  page.dashboardQA = qa;
  page.removeAllListeners("pageerror");
  page.removeAllListeners("console");
  page.on("pageerror", (error) => qa.errors.push(error.message));
  page.on("console", (message) => { if (message.type() === "error") qa.errors.push(message.text()); });
  const GiB = 1024 ** 3;
  const names = ["Hong Kong · Compute", "Tokyo · Storage", "Frankfurt · Packet loss", "London · Offline", "Singapore · New node", "California · HTTP only", "Sydney · Missing metrics", "Osaka · Healthy"];
  const regions = ["🇭🇰", "🇯🇵", "🇩🇪", "🇬🇧", "🇸🇬", "🇺🇸", "🇦🇺", "🇯🇵"];
  const publicRemarks = [
    "MangoHub, 挖矿中枢(gost), miner-knock, content-provenance",
    "MangoDrive, picbed, game-auto-framework",
    "",
    "muse-site, sing-box(Reality)",
    "WARP节点",
    "Emby, MediaWarp",
    "",
    "awesome-ui, EasyTier节点",
  ];
  const nodes = Object.fromEntries(names.map((name, index) => {
    const uuid = index === 0 ? "cd520e0b-bb1a-456e-b58c-7814f83c78f6" : `qa-node-${index}`;
    return [uuid, { uuid, name, region: regions[index], cpu_name: "AMD EPYC", cpu_cores: 4, mem_total: 4 * GiB, disk_total: 40 * GiB, swap_total: GiB, arch: "amd64", os: "Debian", virtualization: "kvm", kernel_version: "6.1", gpu_name: "", group: index < 4 ? "Production" : "Lab", weight: index, price: 5, currency: "$", billing_cycle: 30, tags: "KVM;SSD", public_remark: publicRemarks[index], traffic_limit: 1024 * GiB, traffic_limit_type: "sum", updated_at: "2026-10-11T00:00:00Z", expired_at: "2027-10-11T00:00:00Z", ipv4: "192.0.2.1" }];
  }));
  const task = (name, loss, avg) => ({ name, loss, avg, min: avg, max: avg, latest: avg, tail: 0 });
  const live = Object.fromEntries(Object.values(nodes).filter((_node, index) => index !== 4).map((node) => {
    const index = node.weight;
    return [node.uuid, { client: node.uuid, online: index !== 3, time: "2026-10-11T00:00:00Z", cpu: [95, 25, 20, 99, 0, 10, null, 5][index], ram: index === 6 ? null : 4 * GiB * (index === 0 ? .92 : .4), disk: 40 * GiB * (index === 1 ? .92 : .22), uptime: 86401, net_in: 120000, net_out: 24000, net_total_up: GiB, net_total_down: 2 * GiB, ping: index === 6 ? {} : index === 5 ? {2:task("Website",25,120)} : {1:task("ICMP",[8,0,100,0,0,0,0,0][index],index === 1 || index === 2 ? 0 : 20),2:task("Website",25,120),9:task("CNB-AI-反代",0,90)}, mining: index === 0 ? {algorithm:"xelishashv3",hashrate_1min:1094} : undefined }];
  }));
  const tasks = [{id:1,name:"ICMP",type:"icmp"},{id:2,name:"Website",type:"http"},{id:9,name:"CNB-AI-反代",type:"http"}];
  const history = {
    from:"2026-10-11T00:00:00Z", to:"2026-10-11T01:00:00Z", count:5,
    records:[{task_id:1,time:"2026-10-11T00:01:00Z",value:0},{task_id:1,time:"2026-10-11T00:59:00Z",value:-1},{task_id:2,time:"2026-10-11T00:01:00Z",value:90},{task_id:2,time:"2026-10-11T00:58:00Z",value:-1},{task_id:9,time:"2026-10-11T00:58:00Z",value:90}],
    tasks: tasks.map((item) => ({...item,total:100,avg:20,loss:item.id === 9 ? 0 : 50})),
  };
  await page.route("https://mangohub.test/**", async (route) => {
    const response = await route.fetch({ url: route.request().url().replace("https://mangohub.test", "http://127.0.0.1:4173") });
    await route.fulfill({ response });
  });
  await page.route("**/api/**", async (route) => {
    const request = route.request();
    qa.network.push(request.url());
    const path = request.url().replace(/^https?:\/\/[^/]+/, "").split("?")[0];
    let body;
    if (path === "/api/public") body = {status:"success", data:{sitename:"MangoHub · Infrastructure", private_site:false, theme:"komari-deer", theme_settings:{language:"en",nodeViewMode:"grid"}, record_enabled:true,record_preserve_time:24,ping_record_preserve_time:24}};
    else if (path === "/api/me") body = {logged_in:false};
    else if (path === "/api/rpc2") {
      const rpc = request.postDataJSON();
      qa.requests.push({method:rpc.method, params:rpc.params});
      let result;
      let fail = false;
      if (rpc.method === "common:getNodes") result = nodes;
      else if (rpc.method === "common:getNodesLatestStatus") { result = live; fail = qa.liveFailure; }
      else if (rpc.method === "public:getPublicPingTasks") { result = tasks; fail = qa.metadataFailure; }
      else if (rpc.method === "public:getMiningEarnings") result = {enabled:false};
      else if (rpc.method === "common:getRecords") {
        if (qa.historyDelay) await page.waitForTimeout(qa.historyDelay);
        result = rpc.params.type === "ping" ? history : {count:0, records:[]}; fail = qa.historyFailure;
      } else result = {count:0,records:[],tasks:[]};
      body = fail ? {jsonrpc:"2.0",id:rpc.id,error:{code:-32603,message:"Fixture failure"}} : {jsonrpc:"2.0",id:rpc.id,result};
    } else body = {status:"success",data:{}};
    await route.fulfill({json:body});
  });
  await page.route("https://get.geojs.io/**", (route) => route.fulfill({json:{ip:"192.0.2.25",city:"Local test",organization:"Fixture"}}));
  await page.route("**/ops/**", (route) => route.fulfill({json:{}}));
  await page.addInitScript(() => { if (location.search.includes("fixture_reset=1")) { localStorage.clear(); sessionStorage.clear(); } });
  await page.setViewportSize({width:1440,height:1000});
  await page.goto("https://mangohub.test/?fixture_reset=1");
  await page.getByRole("heading", {name:"Infrastructure overview"}).waitFor();
  await page.locator("[data-node-card]").first().waitFor();
  await page.waitForTimeout(500);
  return { cards: await page.locator("[data-node-card]").count(), requests:qa.requests, errors:qa.errors };
}
