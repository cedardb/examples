-- Query data for a candlestick chart (candlesticks in Grafana)
with limits as (
    select 34200000000000::bigint as start, -- from 9:30 AM (in nanoseconds since midnight), i.e. the start of trading day
           34200000000000 + (30*60)::bigint * 1000 * 1000 * 1000 as end, -- to 30*60 seconds = 30 minutes after market start.
           10::bigint * 1000 * 1000 * 1000 as step  -- bin size of 10 seconds
), bins as (
    -- we always generate the bins from the start of the trading day so they are stable
    select generate_series(l.start ,l.end, l.step) as time
    from limits l 
),
prices as (
  -- for any order of a given stock executed within the relevant time span, find the price
  select 
    e.timestamp as time, s.name as metric, 
    -- if the execution itself has a price, it takes precedence
    max(coalesce(e.price, o.price)) as value,
    max(coalesce(e.price, o.price) * e.quantity) as volume
  from executions e, stocks s, orders o, limits l
  where e.orderid = o.orderid
    and o.stockid = s.stockid 
    and s.name in ('AAPL', 'MSFT') -- stock ticker symbols we're interested in
    and e.timestamp >= l.start
    and e.timestamp < l.end
  group by e.timestamp, s.name
),
binned as (
  select 
      extract(epoch from current_date + (b.time/(1000*1000) * interval '1 millisecond')) as time,
      p.metric, 
      first_value(p.value) over w as open,
      last_value(p.value) over w as close,
      max(p.value) over w as high,
      min(p.value) over w as low,
      sum(p.volume) over w as volume,
      row_number() over w as rn
  from prices p, bins b, limits l
  -- assign each event into its bin
  where p.time >= b.time and p.time < b.time + l.step
  -- for each bin, find the candle stick parameters with a window function
  window w as (partition by b.time, p.metric order by p.time asc rows between unbounded preceding and unbounded following)
)
select metric, time, open, close, high, low, volume 
from binned b 
where rn = 1;
