-- Query data for an orderbook depth chart (trend line in Grafana)
with orderdepth as ( 
    -- count the number of available stock at each price for the buy and for the sell side
    select price, side, sum(quantity) as quantity
    from orderbook o, stocks s
    where o.stockId = s.stockId
    and s.name = 'AAPL' -- stock ticker symbol we're interested in
    group by price, side
    having sum(quantity) > 0
),
cumulative_sell as (
    -- transform the sell side into a cumulative sum with a window query
    select price, side, sum(quantity) over (order by price asc) as sum
    from orderdepth
    where side = 'SELL'
),
cumulative_buy as (
    -- transform the buy side into a cumulative sum with a window query
    select price, side, sum(quantity) over (order by price desc) as sum
    from orderdepth
    where side = 'BUY'

)
-- stitch together buy and sell side
select * 
from (select * from cumulative_sell union all select * from cumulative_buy) 
order by price;
