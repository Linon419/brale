from pandas import DataFrame
from freqtrade.strategy.interface import IStrategy


class BraleRelayStrategy(IStrategy):
    timeframe = "5m"
    minimal_roi = {"0": 0.01}
    stoploss = -0.99
    use_custom_stoploss = False

    def populate_indicators(self, dataframe: DataFrame, metadata: dict) -> DataFrame:
        """
        该策略仅作为 Brale 的 API 执行壳，不做任何指标处理。
        """
        return dataframe

    def populate_entry_trend(self, dataframe: DataFrame, metadata: dict) -> DataFrame:
        """
        禁用 freqtrade 自带的开仓信号，所有开仓都依赖外部 /forceenter。
        """
        dataframe["enter_long"] = 0
        dataframe["enter_short"] = 0
        return dataframe

    def populate_exit_trend(self, dataframe: DataFrame, metadata: dict) -> DataFrame:
        """
        禁用 freqtrade 自带的平仓信号，交由 Brale 控制 /forceexit。
        """
        dataframe["exit_long"] = 0
        dataframe["exit_short"] = 0
        return dataframe
