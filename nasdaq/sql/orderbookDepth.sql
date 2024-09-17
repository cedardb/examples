-- Query data for an orderbook depth chart (trend line in Grafana)
with orderdepth as (
    select price, side, sum(quantity) as quantity
    from orderbook o, stocks s
    where o.stockId = s.stockId
      and s.name = 'AAPL' -- stock ticker symbol you're interested in
    group by price, side
),
     cumulative_sell as (
         select price, side as metric, sum(quantity) over (order by price asc) as sum
         from orderdepth
         where quantity > 0
           and side = 'SELL'
     ),
     cumulative_buy as (
         select price, side as metric, sum(quantity) over (order by price desc) as sum
         from orderdepth
         where quantity > 0
           and side = 'BUY'
     ),
     cumulative as (
         select * from cumulative_sell where sum > 0 and price < 1.05 * (select min(price) from cumulative_sell) -- 1.05 is the width: Show all orders between 1/1.05x and 1.05x of the last trade
         union all
         select * from cumulative_buy where sum > 0 and price > 1::numeric/1.05 * (select max(price) from cumulative_buy) -- width again
     )
select * from cumulative order by price asc;
