-- Query data for a candlestick chart (candlesticks in Grafana)
with limits as (
    select 34200000000000::bigint as start, -- from 9:30 AM (in nanoseconds since midnight), i.e. the start of trading day
           34200000000000 + (30*60)::bigint * 1000 * 1000 * 1000 as end, -- to 30*60 seconds = 30 minutes after market start.
           10::bigint * 1000 * 1000 * 1000 as step  -- bin size of 10 seconds
), bins as (
    select generate_series(l.start ,l.end, l.step) as time
    from limits l -- we always generate the bins from the start of the trading day so they are stable
),
     prices as (
         select e.timestamp as time, s.name as metric, max(coalesce(e.price, o.price)) as value, max(coalesce(e.price, o.price) * e.quantity)  as volume
         from executions e, stocks s, orders o
         where e.orderid = o.orderid
           and o.stockid = s.stockid
           and s.name in ('AAPL', 'MSFT') -- stock ticker symbols you're interested in
         group by e.timestamp, s.name
     ),
     binned as (
         select
             extract(epoch from current_date + (b.time/(1000*1000) * interval '1 millisecond')) as time, -- time as if the stock data was from today
             p.metric,
             first_value(p.value) over w as open,
             last_value(p.value) over w as close,
             max(p.value) over w as high,
             min(p.value) over w as low,
             sum(p.volume) over w as volume,
             row_number() over w as rn
         from prices p, bins b, limits l
         where p.time >= b.time and p.time < b.time + l.step
           and p.time >= l.start
           and p.time < l.end
         window w as (partition by b.time, p.metric order by p.time asc),
     )
select metric as stock, time, open, close, high, low, volume from binned b
where not exists (
    select * from binned b2
    where b2.metric = b.metric
      and b2.time = b.time
      and b2.rn > b.rn
) order by b.metric, b.time asc;
