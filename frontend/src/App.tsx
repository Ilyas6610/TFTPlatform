import { NavLink, Route, Routes } from "react-router-dom";
import ProfilePage from "./pages/ProfilePage";
import MatchDetailPage from "./pages/MatchDetailPage";
import LeaderboardPage from "./pages/LeaderboardPage";
import MetaStatsPage from "./pages/MetaStatsPage";
import SetInfoPage from "./pages/SetInfoPage";
import { CURRENT_TFT_SET } from "./config";

export default function App() {
  return (
    <div className="app">
      <header className="app-header">
        <span className="brand">TFT Stats</span>
        <nav>
          <NavLink to="/" end>
            Profile
          </NavLink>
          <NavLink to="/leaderboard/na1">Leaderboard</NavLink>
          <NavLink to={`/meta/${CURRENT_TFT_SET}`}>Meta</NavLink>
          <NavLink to={`/set/${CURRENT_TFT_SET}/units`}>Set Info</NavLink>
        </nav>
      </header>
      <main className="app-main">
        <Routes>
          <Route path="/" element={<ProfilePage />} />
          <Route path="/players/:region/:name/:tag" element={<ProfilePage />} />
          <Route path="/matches/:matchId" element={<MatchDetailPage />} />
          <Route path="/leaderboard/:platform" element={<LeaderboardPage />} />
          <Route path="/meta/:set" element={<MetaStatsPage />} />
          <Route path="/set/:set/:tab" element={<SetInfoPage />} />
        </Routes>
      </main>
    </div>
  );
}
